/**
 * 主题偏好：dark / light / system，存在 localStorage（不含身份信息）。
 * 默认值由后台「白标 → 配色」决定，SSR 写在 <html data-scheme>；首帧前的切换由 app.html 的内联脚本完成。
 * 这里负责用户在页面上切换时同步 <html> 与地址栏颜色。
 */
export type ThemePreference = 'dark' | 'light' | 'system';

const KEY = 'xs_theme';

function isPreference(value: string | null): value is ThemePreference {
	return value === 'dark' || value === 'light' || value === 'system';
}

/** 当前偏好：内联脚本已经把结果写进 data-theme，没有则退回后台配置的默认值。 */
export function readTheme(): ThemePreference {
	const root = document.documentElement;
	const applied = root.getAttribute('data-theme');
	if (isPreference(applied)) {
		return applied;
	}
	const configured = root.getAttribute('data-scheme');
	return isPreference(configured) ? configured : 'dark';
}

function resolved(preference: ThemePreference): 'dark' | 'light' {
	if (preference === 'system') {
		return matchMedia('(prefers-color-scheme: light)').matches ? 'light' : 'dark';
	}
	return preference;
}

/** 地址栏颜色取当前配色的页面底色（注入的 --d-canvas / --l-canvas，没注入时用内置值）。 */
function themeColor(mode: 'dark' | 'light'): string {
	const value = getComputedStyle(document.documentElement)
		.getPropertyValue(mode === 'light' ? '--l-canvas' : '--d-canvas')
		.trim();
	return value || (mode === 'light' ? '#f6f6f8' : '#0b0b0f');
}

export function applyTheme(preference: ThemePreference) {
	const root = document.documentElement;
	const mode = resolved(preference);
	root.setAttribute('data-theme', preference);
	root.classList.toggle('light', mode === 'light');
	document.querySelector('meta[name="theme-color"]')?.setAttribute('content', themeColor(mode));
	try {
		localStorage.setItem(KEY, preference);
	} catch {
		// 写不进去时只影响下次打开
	}
}

/** 选「跟随系统」时，系统深浅色变化要跟着变。返回取消监听的函数。 */
export function followSystem(): () => void {
	const media = matchMedia('(prefers-color-scheme: light)');
	const onChange = () => {
		if (readTheme() === 'system') {
			applyTheme('system');
		}
	};
	media.addEventListener('change', onChange);
	return () => media.removeEventListener('change', onChange);
}
