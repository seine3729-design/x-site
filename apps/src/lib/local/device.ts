/**
 * 本机设备标识（docs/07 第 0.2 节 X-Device-Id）：首次使用时随机生成并存在 localStorage。
 * 只是随机 UUID，不含身份信息、不关联硬件；用于广告频控、选线种子与播放令牌的设备绑定，
 * 让同一浏览器换网络后（IP 变化）播放令牌仍然有效。
 */
const KEY = 'xs_did';
const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

let cached: string | null = null;

export function deviceId(): string | null {
	if (cached) {
		return cached;
	}
	if (typeof window === 'undefined' || typeof crypto?.randomUUID !== 'function') {
		return null;
	}
	try {
		let value = localStorage.getItem(KEY);
		if (!value || !UUID.test(value)) {
			value = crypto.randomUUID();
			localStorage.setItem(KEY, value);
		}
		cached = value;
	} catch {
		// 隐私模式写不进去：本次会话内用内存里的标识
		cached = crypto.randomUUID();
	}
	return cached;
}
