// Owns ~/.pixeltui/config.json from the Rust side (there is no /api/config
// endpoint — the desktop app is the host). Schema mirrors
// tui/config/config.go:50-64 so the file stays compatible with the Go binary.
// Phase 0 only loads/inspects it; Phase 9 wires the onboarding wizard and
// set_config + sidecar restart.

use anyhow::{Context, Result};
use serde::{Deserialize, Serialize};
use std::path::PathBuf;

#[derive(Debug, Clone, Default, Serialize, Deserialize)]
pub struct Subsonic {
	pub url: String,
	pub user: String,
	pub pass: String,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Charts {
	pub global: bool,
	pub country: String,
}

impl Default for Charts {
	fn default() -> Self {
		Self { global: true, country: String::new() }
	}
}

#[derive(Debug, Clone, Default, Serialize, Deserialize)]
pub struct Scrobble {
	pub enabled: bool,
	pub lastfm_secret: String,
	pub lastfm_session: String,
	pub lastfm_user: String,
	pub listenbrainz_token: String,
}

#[derive(Debug, Clone, Default, Serialize, Deserialize)]
pub struct Server {
	#[serde(default, skip_serializing_if = "String::is_empty")] pub addr: String,
	#[serde(default, skip_serializing_if = "String::is_empty")] pub name: String,
	#[serde(default, skip_serializing_if = "String::is_empty")] pub public_url: String,
	#[serde(default, skip_serializing_if = "String::is_empty")] pub tunnel: String,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(default)]
pub struct Config {
	pub lastfm_key: String,
	pub scrobble: Scrobble,
	pub subsonic: Subsonic,
	pub local_dirs: Vec<String>,
	pub download_dir: String,
	pub theme: String,
	pub explore: i32,
	pub autoplay: bool,
	pub seek_step: i32,
	pub charts: Charts,
	pub server: Server,
	pub acoustid_api_key: String,
	pub audio_device: String,
}

impl Default for Config {
	fn default() -> Self {
		Self {
			lastfm_key: String::new(),
			scrobble: Default::default(),
			subsonic: Default::default(),
			local_dirs: Vec::new(),
			download_dir: String::new(),
			theme: String::new(),
			explore: 5,
			autoplay: true,
			seek_step: 10,
			charts: Charts::default(),
			server: Default::default(),
			acoustid_api_key: String::new(),
			audio_device: String::new(),
		}
	}
}

pub fn data_dir() -> Result<PathBuf> {
	let home = dirs::home_dir().context("no home directory")?;
	Ok(home.join(".pixeltui"))
}

pub fn config_path() -> Result<PathBuf> {
	Ok(data_dir()?.join("config.json"))
}

pub fn exists() -> bool {
	config_path().map(|p| p.exists()).unwrap_or(false)
}

pub fn load() -> Result<Config> {
	let p = config_path()?;
	if !p.exists() {
		return Ok(Config::default());
	}
	let data = std::fs::read(&p).with_context(|| format!("read {}", p.display()))?;
	let mut cfg: Config = serde_json::from_slice(&data).unwrap_or_default();
	if cfg.seek_step <= 0 {
		cfg.seek_step = 10;
	}
	Ok(cfg)
}

pub fn save(cfg: &Config) -> Result<()> {
	let p = config_path()?;
	if let Some(parent) = p.parent() {
		std::fs::create_dir_all(parent)?;
	}
	let data = serde_json::to_vec_pretty(cfg)?;
	std::fs::write(&p, data).with_context(|| format!("write {}", p.display()))?;
	Ok(())
}