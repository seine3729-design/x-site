/** 轻提示：3.5 秒自动消失，读屏以 polite 方式播报，不抢焦点。 */
export type Toast = { id: number; text: string; tone: 'info' | 'success' | 'error'; action?: { label: string; run: () => void } };

class Toasts {
	items = $state<Toast[]>([]);
	#seq = 0;

	show(text: string, options: { tone?: Toast['tone']; action?: Toast['action']; duration?: number } = {}) {
		const id = ++this.#seq;
		this.items = [...this.items.slice(-2), { id, text, tone: options.tone ?? 'info', action: options.action }];
		setTimeout(() => this.dismiss(id), options.duration ?? 3500);
		return id;
	}

	dismiss(id: number) {
		this.items = this.items.filter((t) => t.id !== id);
	}
}

export const toasts = new Toasts();
