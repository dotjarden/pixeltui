mod commands;
mod config;
mod downloads;
mod keychain;
mod provision;
mod sidecar;

use commands::AppState;

#[cfg_attr(mobile, tauri::mobile_entry_point)]
pub fn run() {
	let log_plugin = tauri_plugin_log::Builder::default()
		.level(log::LevelFilter::Info)
		.build();

	let app = tauri::Builder::default()
		.plugin(tauri_plugin_shell::init())
		.plugin(log_plugin)
		.plugin(tauri_plugin_global_shortcut::Builder::new().build())
		.plugin(tauri_plugin_process::init())
		.plugin(tauri_plugin_updater::Builder::new().build())
		.manage(AppState {
			sidecar: std::sync::Mutex::new(None),
			token: std::sync::Mutex::new(None),
			downloads: std::sync::Mutex::new(std::collections::HashMap::new()),
		})
		.setup(|app| {
			// Spawn the pixeltui sidecar (companion server) on loopback and
			// auto-pair with it. Pipes (non-TTY) make the server skip its
			// interactive first-run onboarding.
			sidecar::start(app.handle().clone())?;
			Ok(())
		})
		.invoke_handler(tauri::generate_handler![
			commands::get_server_base,
			commands::get_token,
			commands::re_pair,
			commands::get_config,
			commands::config_exists,
			commands::set_config,
			commands::restart_sidecar,
			downloads::download_track,
			downloads::cancel_download,
			downloads::list_downloads,
			downloads::remove_download,
			downloads::remove_all_downloads,
			downloads::download_file_path,
			downloads::downloads_dir_path,
			provision::provision,
		])
		.build(tauri::generate_context!())
		.expect("error while building tauri application");

	app.run(|app_handle, event| {
		if matches!(event, tauri::RunEvent::Exit | tauri::RunEvent::ExitRequested { .. }) {
			sidecar::stop(app_handle);
		}
	});
}