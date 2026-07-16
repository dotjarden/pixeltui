import { check } from '@tauri-apps/plugin-updater';
import { relaunch } from '@tauri-apps/plugin-process';

export async function initUpdater(): Promise<void> {
  try {
    const update = await check();
    if (update?.available) {
      console.log('update available:', update.version, update.date, update.body);
      await update.downloadAndInstall((event) => {
        switch (event.event) {
          case 'Started':
            console.log('update download started, content length', event.data.contentLength ?? 'unknown');
            break;
          case 'Progress':
            break;
          case 'Finished':
            console.log('update download finished');
            break;
        }
      });
      await relaunch();
    }
  } catch (err) {
    console.error('updater check failed:', err);
  }
}
