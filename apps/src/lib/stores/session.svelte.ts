/**
 * 浏览器侧的登录状态（只在浏览器里填充；SSR 时始终是 unknown，公共页面的 HTML 不随登录态变化）。
 * 令牌在 HttpOnly Cookie 里读不到，这里只根据脚本可读的提示 Cookie xs_li 决定要不要请求 /me/state。
 */
import type { MeState } from '#lib/api/types.ts';

const HINT = 'xs_li';

export function hasLoginHint(): boolean {
	return typeof document !== 'undefined' && document.cookie.split('; ').some((c) => c === `${HINT}=1`);
}

function dropHint() {
	document.cookie = `${HINT}=; Path=/; Max-Age=0; SameSite=Lax`;
}

class Session {
	status = $state<'unknown' | 'guest' | 'user'>('unknown');
	me = $state<MeState | null>(null);
	#loading: Promise<void> | null = null;

	get loggedIn() {
		return this.status === 'user';
	}

	/** 首次挂载与登录、登出后调用；重复调用合并为一次请求。 */
	refresh(force = false): Promise<void> {
		if (!force && this.#loading) {
			return this.#loading;
		}
		this.#loading = this.#load().finally(() => {
			this.#loading = null;
		});
		return this.#loading;
	}

	async #load() {
		if (!hasLoginHint()) {
			this.status = 'guest';
			this.me = null;
			return;
		}
		try {
			const { api, isAuthError } = await import('#lib/api/client.ts');
			try {
				this.me = await api.get<MeState>('/api/v1/me/state');
				this.status = 'user';
			} catch (err) {
				if (isAuthError(err)) {
					dropHint();
					this.me = null;
					this.status = 'guest';
					return;
				}
				// 网络或服务异常：保留「已登录」的判断，个人数据稍后重试
				this.status = 'user';
			}
		} catch {
			this.status = 'guest';
		}
	}

	setUnread(count: number) {
		if (this.me) {
			this.me = { ...this.me, unread: Math.max(0, count) };
		}
	}
}

export const session = new Session();
