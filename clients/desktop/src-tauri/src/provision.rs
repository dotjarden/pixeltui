// First-run provisioning: run a one-shot `pixeltui doctor --fix` (the bundled
// sidecar binary's own self-repair subcommand) to install yt-dlp + mpv into
// ~/.pixeltui/. stdout/stderr are streamed to the frontend as
// `provision://stdout` / `provision://stderr` events so the onboarding wizard
// can show live progress.
//
// ffmpeg/ffprobe (which `doctor` does NOT install) are left as a manual step —
// they're only needed for local-file artwork (`/api/art` `lo:`) and the yt
// transcode fallback, neither of which blocks normal playback (yt uses
// InnerTube, local files stream directly). A future phase can fetch static
// evermeet (mac) / BtbN (windows) builds into ~/.pixeltui/bin.

use tauri::{AppHandle, Emitter};
use tauri_plugin_shell::process::CommandEvent;
use tauri_plugin_shell::ShellExt;

/// Run `pixeltui doctor --fix` and stream its output. Returns the full stdout.
#[tauri::command]
pub async fn provision(app: AppHandle) -> Result<String, String> {
	let (mut rx, child) = app
		.shell()
		.sidecar("pixeltui")
		.map_err(|e| format!("resolve sidecar: {e}"))?
		.args(["doctor", "--fix"])
		.spawn()
		.map_err(|e| format!("spawn doctor: {e}"))?;
	let _child = child; // keep alive until the process exits

	let mut out = String::new();
	while let Some(event) = rx.recv().await {
		match event {
			CommandEvent::Stdout(bytes) => {
				let line = String::from_utf8_lossy(&bytes).to_string();
				out.push_str(&line);
				let _ = app.emit("provision://stdout", &line);
			}
			CommandEvent::Stderr(bytes) => {
				let line = String::from_utf8_lossy(&bytes).to_string();
				out.push_str(&line);
				let _ = app.emit("provision://stderr", &line);
			}
			CommandEvent::Terminated(status) => {
				let _ = app.emit("provision://done", format!("{status:?}"));
				break;
			}
			_ => {}
		}
	}
	Ok(out)
}