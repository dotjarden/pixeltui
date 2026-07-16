// Sidecar lifecycle: spawn the bundled `pixeltui serve` binary on loopback,
// scrape its pairing code from stdout, wait for /health, and auto-pair with a
// bearer token stored in the OS keychain. Mirrors the iOS pairing flow but
// drives it from Rust since the desktop app *is* the host.

use anyhow::{anyhow, Context, Result};
use std::process::Command;
use std::thread;
use std::time::Duration;
use tauri::{AppHandle, Emitter, Manager, State};
use tauri_plugin_shell::process::CommandEvent;
use tauri_plugin_shell::ShellExt;
use tokio::sync::oneshot;

use crate::commands::AppState;

/// Default loopback address for the embedded server. Used when `server.addr`
/// is unset in config. 8790 coexists with a possibly-already-running
/// `pixeltui serve` on 8787.
pub const SIDECAR_ADDR: &str = "127.0.0.1:8790";
const DEVICE_NAME: &str = "PixelPal Desktop";

/// The bind address for the sidecar, from `server.addr` (config-driven) or the
/// default. Empty config → default.
pub fn sidecar_addr() -> String {
	match crate::config::load() {
		Ok(c) if !c.server.addr.is_empty() => c.server.addr.clone(),
		_ => SIDECAR_ADDR.to_string(),
	}
}

/// Base URL the frontend + orchestrator use to talk to the sidecar. A `:port`
/// bind (all interfaces) is reached via 127.0.0.1.
pub fn sidecar_base() -> String {
	let addr = sidecar_addr();
	if addr.starts_with(':') {
		format!("http://127.0.0.1{}", addr)
	} else {
		format!("http://{}", addr)
	}
}

/// Extract the 6-char pairing code from a stdout line like `  Code: AB23CD`.
/// The server's alphabet is ABCDEFGHJKLMNPQRSTUVWXYZ23456789 (no I/O/0/1).
fn scrape_code(line: &str) -> Option<String> {
	let rest = line.trim_start().strip_prefix("Code:")?;
	let code = rest.trim();
	if code.len() == 6 && code.chars().all(|c| c.is_ascii_uppercase() || c.is_ascii_digit())
		&& !code.chars().any(|c| "IO01".contains(c))
	{
		Some(code.to_string())
	} else {
		None
	}
}

pub fn start(app: AppHandle) -> Result<()> {
	let cfg = crate::config::load().unwrap_or_default();
	let addr = if cfg.server.addr.is_empty() {
		SIDECAR_ADDR.to_string()
	} else {
		cfg.server.addr.clone()
	};
	let base = if addr.starts_with(':') {
		format!("http://127.0.0.1{}", addr)
	} else {
		format!("http://{}", addr)
	};

	// A desktop crash can orphan the bundled Go child. On the next launch that
	// old process still owns the loopback port, making every apparent restart
	// silently talk to yesterday's server. Reclaim only an identified local
	// `pixeltui serve` listener; never touch another application or LAN host.
	reclaim_stale_local_sidecar(&addr);

	// Build the serve flags from config: --addr (always), --tunnel (config or
	// empty = LAN only), --name (only if set). The Go binary loads config.json
	// itself, so a restart picks up every other field automatically.
	let mut args: Vec<String> = vec!["serve".into(), "--addr".into(), addr.clone()];
	args.push("--tunnel".into());
	args.push(cfg.server.tunnel.clone());
	if !cfg.server.name.is_empty() {
		args.push("--name".into());
		args.push(cfg.server.name.clone());
	}

	let (mut rx, child) = app
		.shell()
		.sidecar("pixeltui")
		.map_err(|e| anyhow!("resolve sidecar: {e}"))?
		.args(args)
		.spawn()
		.map_err(|e| anyhow!("spawn sidecar: {e}"))?;

	{
		let state: State<AppState> = app.state();
		*state.sidecar.lock().unwrap() = Some(child);
	}

	let (code_tx, code_rx) = oneshot::channel::<String>();

	// Reader: forward stdout/stderr to the frontend and capture the pairing code.
	let app_reader = app.clone();
	tauri::async_runtime::spawn(async move {
		let mut code_tx = Some(code_tx);
		while let Some(event) = rx.recv().await {
			match event {
				CommandEvent::Stdout(bytes) => {
					let line = String::from_utf8_lossy(&bytes).to_string();
					let _ = app_reader.emit("sidecar://stdout", &line);
					if let Some(code) = scrape_code(&line) {
						if let Some(tx) = code_tx.take() {
							let _ = tx.send(code);
						}
					}
				}
				CommandEvent::Stderr(bytes) => {
					let line = String::from_utf8_lossy(&bytes).to_string();
					let _ = app_reader.emit("sidecar://stderr", &line);
				}
				CommandEvent::Terminated(_) => {
					let _ = app_reader.emit("sidecar://terminated", ());
					break;
				}
				_ => {}
			}
		}
	});

	// Orchestrator: wait for health, reuse or mint a token, signal readiness.
	let app_orch = app.clone();
	tauri::async_runtime::spawn(async move {
		if let Err(e) = orchestrate(app_orch.clone(), code_rx, base).await {
			log::error!("sidecar orchestration failed: {e:#}");
			let _ = app_orch.emit("app://error", format!("{e:#}"));
		}
	});

	Ok(())
}

#[cfg(unix)]
fn reclaim_stale_local_sidecar(addr: &str) {
	let is_local = addr.starts_with("127.0.0.1:") || addr.starts_with("localhost:") || addr.starts_with(':');
	if !is_local {
		return;
	}
	let Some(port) = addr.rsplit(':').next().filter(|p| !p.is_empty()) else {
		return;
	};
	let output = match Command::new("lsof")
		.args(["-nP", &format!("-iTCP:{port}"), "-sTCP:LISTEN", "-t"])
		.output()
	{
		Ok(output) => output,
		Err(_) => return,
	};
	for pid in String::from_utf8_lossy(&output.stdout).lines().map(str::trim).filter(|pid| !pid.is_empty()) {
		let command = Command::new("ps")
			.args(["-p", pid, "-o", "command="])
			.output()
			.ok()
			.map(|out| String::from_utf8_lossy(&out.stdout).to_string())
			.unwrap_or_default();
		if command.contains("pixeltui") && command.contains("serve") {
			let _ = Command::new("kill").args(["-TERM", pid]).status();
		}
	}
	// Give the kernel a short moment to release an orphaned listener before
	// spawning the replacement sidecar.
	thread::sleep(Duration::from_millis(180));
}

#[cfg(not(unix))]
fn reclaim_stale_local_sidecar(_addr: &str) {}

pub fn stop(app: &AppHandle) {
	let state: State<AppState> = app.state();
	let child = state.sidecar.lock().unwrap().take();
	if let Some(child) = child {
		let _ = child.kill();
	}
}

/// Restart the sidecar (stop + start). Used after a config save so the Go
/// binary re-reads config.json. The token persists in the keychain, so no
/// re-pair is needed — `orchestrate` reuses it.
pub fn restart(app: AppHandle) -> Result<()> {
	stop(&app);
	start(app)
}

async fn orchestrate(app: AppHandle, code_rx: oneshot::Receiver<String>, base: String) -> Result<()> {
	wait_health(&base).await?;
	let _ = app.emit("app://health", "ok");

	// Reuse an existing token if it still validates.
	if let Ok(tok) = crate::keychain::get_token() {
		if validate_token(&base, &tok).await {
			set_token(&app, tok.clone());
			let _ = app.emit("app://ready", &tok);
			return Ok(());
		}
		let _ = crate::keychain::delete_token();
	}

	// Otherwise pair on loopback using the scraped code (single-use, so pair
	// before any other client does).
	let _ = app.emit("app://status", "pairing");
	let code = code_rx.await.context("sidecar did not emit a pairing code")?;
	let token = pair(&base, &code).await?;
	crate::keychain::set_token(&token)?;
	set_token(&app, token.clone());
	let _ = app.emit("app://ready", &token);
	Ok(())
}

fn set_token(app: &AppHandle, token: String) {
	let state: State<AppState> = app.state();
	*state.token.lock().unwrap() = Some(token);
}

async fn wait_health(base: &str) -> Result<()> {
	let client = reqwest::Client::builder()
		.timeout(Duration::from_secs(2))
		.build()?;
	for _ in 0..80 {
		if let Ok(resp) = client.get(format!("{base}/health")).send().await {
			if resp.status().is_success() {
				return Ok(());
			}
		}
		tokio::time::sleep(Duration::from_millis(250)).await;
	}
	Err(anyhow!("server never became healthy at {base}"))
}

async fn validate_token(base: &str, token: &str) -> bool {
	let Ok(client) = reqwest::Client::builder().timeout(Duration::from_secs(3)).build() else {
		return false;
	};
	client
		.get(format!("{base}/api/sources"))
		.header("Authorization", format!("Bearer {token}"))
		.send()
		.await
		.map(|r| r.status().is_success())
		.unwrap_or(false)
}

#[derive(serde::Deserialize)]
struct PairResp {
	token: String,
}

async fn pair(base: &str, code: &str) -> Result<String> {
	let client = reqwest::Client::builder()
		.timeout(Duration::from_secs(5))
		.build()?;
	let resp = client
		.post(format!("{base}/pair"))
		.json(&serde_json::json!({ "code": code, "name": DEVICE_NAME }))
		.send()
		.await?;
	if !resp.status().is_success() {
		return Err(anyhow!("pair failed: HTTP {}", resp.status()));
	}
	let body: PairResp = resp.json().await.context("parse pair response")?;
	Ok(body.token)
}
