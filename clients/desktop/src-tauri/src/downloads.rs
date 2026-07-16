// Client-side downloads. There is no server-side download endpoint — the
// desktop saves the bytes of `GET /api/stream?id=&token=` itself into
// `~/.pixeltui/downloads/<sanitized-id>.<ext>`, plus an `index.json` manifest.
// Offline playback then loads the file via the Tauri asset protocol
// (convertFileSrc) instead of streaming. Mirrors iOS `DownloadStore`.

use anyhow::{Context, Result};
use futures_util::StreamExt;
use serde::{Deserialize, Serialize};
use std::path::PathBuf;
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::Arc;
use tauri::Emitter;
use tokio::io::AsyncWriteExt;

use crate::commands::AppState;
use crate::sidecar::sidecar_base;

/// Where downloaded audio lives. The desktop owns `~/.pixeltui`, so this is its
/// own downloads dir — NOT the Go config's `download_dir` (that's the
/// yt-dlp/TUI path). iOS uses the same `~/.pixeltui/downloads` layout.
pub fn downloads_dir() -> Result<PathBuf> {
    Ok(crate::config::data_dir()?.join("downloads"))
}

fn index_path() -> Result<PathBuf> {
    Ok(downloads_dir()?.join("index.json"))
}

/// Metadata the frontend supplies to start a download.
#[derive(Debug, Clone, Deserialize)]
pub struct DownloadRequest {
    pub id: String,
    pub track: String,
    pub artist: String,
    #[serde(default)]
    pub album: String,
    #[serde(default)]
    pub duration: f64,
    #[serde(default)]
    pub art: String,
}

/// A persisted/returned download entry (manifest row).
#[derive(Debug, Clone, Serialize, Deserialize)]
#[allow(non_snake_case)]
pub struct DownloadEntry {
    pub id: String,
    pub track: String,
    pub artist: String,
    pub album: String,
    pub duration: f64,
    pub art: String,
    pub fileName: String,
    pub bytes: u64,
}

#[derive(Clone, Serialize)]
struct ProgressPayload<'a> {
    id: &'a str,
    fraction: f64,
}

#[derive(Clone, Serialize)]
struct DonePayload {
    entry: DownloadEntry,
}

#[derive(Clone, Serialize)]
struct ErrorPayload<'a> {
    id: &'a str,
    message: String,
}

/// Sanitize a track id into a safe file stem (mirror the Go server's rule:
/// non-`[A-Za-z0-9_-]` → `_`).
fn sanitize(id: &str) -> String {
    id.chars()
        .map(|c| {
            if c.is_ascii_alphanumeric() || c == '_' || c == '-' {
                c
            } else {
                '_'
            }
        })
        .collect()
}

/// Pick a file extension from the stream's Content-Type. Mirrors the Go
/// `/api/stream` MIME→ext mapping (default m4a).
fn ext_for_mime(mime: &str) -> &'static str {
    let m = mime.split(';').next().unwrap_or("").trim().to_ascii_lowercase();
    match m.as_str() {
        "audio/mpeg" | "audio/mp3" => "mp3",
        "audio/flac" => "flac",
        "audio/ogg" | "application/ogg" => "ogg",
        "audio/wav" | "audio/x-wav" => "wav",
        "audio/aac" | "audio/aacp" => "aac",
        "audio/mp4" | "audio/m4a" | "audio/x-m4a" => "m4a",
        _ => "m4a",
    }
}

/// Load the manifest (empty list if missing/corrupt).
fn load_index() -> Vec<DownloadEntry> {
    match index_path() {
        Ok(p) if p.exists() => {
            std::fs::read(&p)
                .ok()
                .and_then(|b| serde_json::from_slice(&b).ok())
                .unwrap_or_default()
        }
        _ => Vec::new(),
    }
}

/// Atomically rewrite the manifest.
fn save_index(entries: &[DownloadEntry]) -> Result<()> {
    let p = index_path()?;
    std::fs::create_dir_all(p.parent().unwrap_or(&p))?;
    let data = serde_json::to_vec_pretty(entries)?;
    std::fs::write(&p, data).with_context(|| format!("write {}", p.display()))?;
    Ok(())
}

/// Upsert an entry by id.
fn upsert(entry: DownloadEntry) -> Result<()> {
    let mut entries = load_index();
    if let Some(e) = entries.iter_mut().find(|e| e.id == entry.id) {
        *e = entry.clone();
    } else {
        entries.push(entry);
    }
    save_index(&entries)
}

fn remove_index(id: &str) -> Result<()> {
    let mut entries = load_index();
    entries.retain(|e| e.id != id);
    save_index(&entries)
}

/// Resolve the on-disk path for a downloaded id (any extension).
pub fn path_for_id(id: &str) -> Result<Option<PathBuf>> {
    let dir = downloads_dir()?;
    let stem = sanitize(id);
    for ext in ["mp3", "flac", "ogg", "wav", "aac", "m4a"] {
        let p = dir.join(format!("{}.{}", stem, ext));
        if p.exists() {
            return Ok(Some(p));
        }
    }
    Ok(None)
}

/// Stream `/api/stream` to disk, emitting progress/done/error events.
/// Cancellable via `cancel_download`.
#[tauri::command]
pub async fn download_track(
    app: tauri::AppHandle,
    state: tauri::State<'_, AppState>,
    req: DownloadRequest,
) -> Result<DownloadEntry, String> {
    let dir = downloads_dir().map_err(|e| e.to_string())?;
    std::fs::create_dir_all(&dir).map_err(|e| e.to_string())?;

    let token = state
        .token
        .lock()
        .unwrap()
        .clone()
        .ok_or_else(|| "no token".to_string())?;

    // Register a cancel flag for this id.
    let cancel = Arc::new(AtomicBool::new(false));
    state
        .downloads
        .lock()
        .unwrap()
        .insert(req.id.clone(), cancel.clone());

    let result = run_download(&app, &dir, &token, &req, &cancel).await;

    // Always remove the in-flight flag.
    state.downloads.lock().unwrap().remove(&req.id);

    match result {
        Ok(entry) => Ok(entry),
        Err(e) => {
            let _ = app.emit(
                "download://error",
                ErrorPayload {
                    id: &req.id,
                    message: e.to_string(),
                },
            );
            Err(e.to_string())
        }
    }
}

async fn run_download(
    app: &tauri::AppHandle,
    dir: &std::path::Path,
    token: &str,
    req: &DownloadRequest,
    cancel: &Arc<AtomicBool>,
) -> Result<DownloadEntry> {
    let url = format!(
        "{}/api/stream?id={}&token={}",
        sidecar_base(),
        urlencoding::encode(&req.id),
        urlencoding::encode(token),
    );
    let client = reqwest::Client::builder()
        .build()
        .context("build client")?;
    let resp = client
        .get(&url)
        .header("Authorization", format!("Bearer {}", token))
        .send()
        .await
        .context("request stream")?;
    if !resp.status().is_success() {
        anyhow::bail!("stream returned {}", resp.status());
    }
    let total = resp.content_length();
    let ext = resp
        .headers()
        .get(reqwest::header::CONTENT_TYPE)
        .and_then(|v| v.to_str().ok())
        .map(ext_for_mime)
        .unwrap_or("m4a");

    let stem = sanitize(&req.id);
    let final_path = dir.join(format!("{}.{}", stem, ext));
    let part_path = dir.join(format!("{}.part", stem));

    let mut file = tokio::fs::File::create(&part_path)
        .await
        .with_context(|| format!("create {}", part_path.display()))?;
    let mut stream = resp.bytes_stream();
    let mut got: u64 = 0;
    let mut last_pct: u32 = 0;

    while let Some(chunk) = stream.next().await {
        if cancel.load(Ordering::Relaxed) {
            drop(file);
            let _ = tokio::fs::remove_file(&part_path).await;
            anyhow::bail!("cancelled");
        }
        let chunk = chunk.context("read chunk")?;
        file.write_all(&chunk)
            .await
            .context("write chunk")?;
        got += chunk.len() as u64;
        if let Some(t) = total {
            if t > 0 {
                let pct = ((got * 100) / t) as u32;
                if pct != last_pct {
                    last_pct = pct;
                    let _ = app.emit(
                        "download://progress",
                        ProgressPayload {
                            id: &req.id,
                            fraction: got as f64 / t as f64,
                        },
                    );
                }
            }
        }
    }
    file.flush().await.context("flush")?;
    drop(file);

    // .part → final name (replace any prior copy under a different ext).
    let _ = tokio::fs::remove_file(&final_path).await;
    tokio::fs::rename(&part_path, &final_path)
        .await
        .with_context(|| format!("rename to {}", final_path.display()))?;

    let bytes = tokio::fs::metadata(&final_path)
        .await
        .map(|m| m.len())
        .unwrap_or(got);
    let file_name = final_path
        .file_name()
        .and_then(|n| n.to_str())
        .unwrap_or(&stem)
        .to_string();

    let entry = DownloadEntry {
        id: req.id.clone(),
        track: req.track.clone(),
        artist: req.artist.clone(),
        album: req.album.clone(),
        duration: req.duration,
        art: req.art.clone(),
        fileName: file_name,
        bytes,
    };
    upsert(entry.clone()).context("update manifest")?;

    let _ = app.emit("download://done", DonePayload { entry: entry.clone() });
    Ok(entry)
}

/// Cancel an in-flight download (no-op if not running).
#[tauri::command]
pub fn cancel_download(state: tauri::State<'_, AppState>, id: String) -> Result<(), String> {
    if let Some(flag) = state.downloads.lock().unwrap().get(&id) {
        flag.store(true, Ordering::Relaxed);
    }
    Ok(())
}

/// All persisted download entries.
#[tauri::command]
pub fn list_downloads() -> Result<Vec<DownloadEntry>, String> {
    Ok(load_index())
}

/// Delete one download's file + manifest row.
#[tauri::command]
pub fn remove_download(id: String) -> Result<(), String> {
    if let Some(p) = path_for_id(&id).map_err(|e| e.to_string())? {
        let _ = std::fs::remove_file(&p);
    }
    remove_index(&id).map_err(|e| e.to_string())
}

/// Delete every download (files + manifest).
#[tauri::command]
pub fn remove_all_downloads() -> Result<(), String> {
    let dir = downloads_dir().map_err(|e| e.to_string())?;
    if dir.exists() {
        for e in std::fs::read_dir(&dir).map_err(|e| e.to_string())?.flatten() {
            let p = e.path();
            if p.is_file() && p.file_name().and_then(|n| n.to_str()) != Some("index.json") {
                let _ = std::fs::remove_file(&p);
            }
        }
    }
    save_index(&[]).map_err(|e| e.to_string())
}

/// Absolute path of a downloaded file (for convertFileSrc), or null.
#[tauri::command]
pub fn download_file_path(id: String) -> Result<Option<String>, String> {
    Ok(path_for_id(&id)
        .map_err(|e| e.to_string())?
        .and_then(|p| p.to_str().map(|s| s.to_string())))
}

/// Expose the downloads dir path (for display / "reveal in files").
#[tauri::command]
pub fn downloads_dir_path() -> Result<String, String> {
    downloads_dir()
        .map(|p| p.to_string_lossy().into_owned())
        .map_err(|e| e.to_string())
}

// Minimal URL-encoding for the id + token query params (avoids pulling a crate).
mod urlencoding {
    pub fn encode(s: &str) -> String {
        let mut out = String::with_capacity(s.len());
        for b in s.bytes() {
            match b {
                b'A'..=b'Z' | b'a'..=b'z' | b'0'..=b'9' | b'-' | b'_' | b'.' | b'~' => {
                    out.push(b as char);
                }
                _ => {
                    out.push('%');
                    out.push_str(&format!("{:02X}", b));
                }
            }
        }
        out
    }
}