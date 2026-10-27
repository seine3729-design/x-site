/**
 * 广告弹窗 / 浮窗的「今天不再显示」记录（docs/04 C.5）。
 * 只存本机偏好，不含任何身份信息；曝光频控由后端按设备按天控制，这里只是别一天内反复弹。
 */
const PREFIX = 'xs_ad_';

function today(now = new Date()): string {
	return now.toISOString().slice(0, 10);
}

export function dismissedToday(key: string, now?: Date): boolean {
	try {
		return localStorage.getItem(PREFIX + key) === today(now);
	} catch {
		return false; // 隐私模式下读不到：当作没关过，显示一次
	}
}

export function dismissToday(key: string, now?: Date): void {
	try {
		localStorage.setItem(PREFIX + key, today(now));
	} catch {
		// 存不下就算了，下次还会显示
	}
}
