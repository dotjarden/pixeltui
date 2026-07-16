use std::collections::HashMap;
use std::sync::atomic::AtomicBool;
use std::sync::{Arc, Mutex};
use tauri::Manager;
use tauri_plugin_shell::process::CommandChild;

/// Shared app state.
pub struct AppState {
	pub sidecar: Mutex<Option<CommandChild>>,
	pub token: Mutex<Option<String>>,
	/// In-flight download cancel flags, keyed by track id.
	pub downloads: Mutex<HashMap<String, Arc<AtomicBool>>>,
}

/// Base URL of the embedded server (used by the frontend for fetch/EventSource).
#[tauri::command]
pub fn get_server_base() -> String {
	crate::sidecar::sidecar_base()
}

/// Current bearer token (if any), for the frontend to use in API calls.
#[tauri::command]
pub fn get_token(state: tauri::State<AppState>) -> Option<String> {
	state.token.lock().unwrap().clone()
}

/// Drop the stored token and restart the sidecar so a fresh pairing code is
/// emitted and we re-pair. Used after a 401 / manual re-pair.
#[tauri::command]
pub async fn re_pair(app: tauri::AppHandle) -> Result<String, String> {
	let _ = crate::keychain::delete_token();
	if let Some(s) = app.try_state::<AppState>() {
		*s.token.lock().unwrap() = None;
	}
	crate::sidecar::stop(&app);
	crate::sidecar::start(app.clone()).map_err(|e| e.to_string())?;
	Ok("restarting".to_string())
}

/// Load the on-disk `~/.pixeltui/config.json`.
#[tauri::command]
pub fn get_config() -> Result<crate::config::Config, String> {
	crate::config::load().map_err(|e| e.to_string())
}

/// Whether a config file already exists (used to decide first-run onboarding).
#[tauri::command]
pub fn config_exists() -> bool {
	crate::config::exists()
}

/// Save config and restart the sidecar so the Go binary re-reads it.
#[tauri::command]
pub async fn set_config(app: tauri::AppHandle, cfg: crate::config::Config) -> Result<(), String> {
	crate::config::save(&cfg).map_err(|e| e.to_string())?;
	crate::sidecar::restart(app.clone()).map_err(|e| e.to_string())?;
	Ok(())
}

/// Restart the sidecar without changing config. Used when the user toggles
/// settings that need a re-read (e.g. server name / tunnel).
#[tauri::command]
pub async fn restart_sidecar(app: tauri::AppHandle) -> Result<(), String> {
	crate::sidecar::restart(app.clone()).map_err(|e| e.to_string())
}