// Bearer-token storage in the OS keychain (macOS Keychain / Windows Credential
// Manager) via the `keyring` crate. Mirrors iOS Keychain usage.

use anyhow::Result;
use keyring::Entry;

const SERVICE: &str = "com.dotjarden.pixeltui";
const ACCOUNT: &str = "default";

fn entry() -> Result<Entry> {
	Entry::new(SERVICE, ACCOUNT).map_err(|e| anyhow::anyhow!("keyring entry: {e:?}"))
}

pub fn set_token(token: &str) -> Result<()> {
	entry()?.set_password(token).map_err(|e| anyhow::anyhow!("set token: {e:?}"))
}

pub fn get_token() -> Result<String> {
	entry()?.get_password().map_err(|e| anyhow::anyhow!("get token: {e:?}"))
}

pub fn delete_token() -> Result<()> {
	match entry()?.delete_credential() {
		Ok(()) => Ok(()),
		Err(keyring::Error::NoEntry) => Ok(()),
		Err(e) => Err(anyhow::anyhow!("delete token: {e:?}")),
	}
}