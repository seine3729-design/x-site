<script lang="ts">
	/**
	 * 分享面板（docs/04 C.7）：复制链接、系统分享、生成并下载海报。
	 * 手机从底部滑上来，电脑是居中弹窗（`#lib/ui/Sheet.svelte` 的 center）。
	 * 海报在面板打开后才画（Canvas + 仓库内的二维码编码器），不进首屏。
	 */
	import { getI18n } from '#lib/i18n/context.ts';
	import { imageSources } from '#lib/media/image.ts';
	import { featureOn, getSite } from '#lib/site.ts';
	import { toasts } from '#lib/stores/toast.svelte.ts';
	import Icon from '#lib/ui/Icon.svelte';
	import Sheet from '#lib/ui/Sheet.svelte';
	import { POSTER_HEIGHT, POSTER_WIDTH, posterFileName, type PosterOptions } from './poster.ts';

	type Props = {
		open: boolean;
		onclose: () => void;
		/** 剧名 */
		title: string;
		/** 「第 3 集」之类的副标题，详情页分享时不传 */
		subtitle?: string;
		/** 要分享的完整地址 */
		url: string;
		/** 封面图片 key 或完整地址 */
		cover: string;
	};
	let { open, onclose, title, subtitle, url, cover }: Props = $props();

	const i18n = getI18n();
	const site = $derived(getSite());
	const posterOn = $derived(featureOn(site.features, 'share_poster'));

	let canvas = $state<HTMLCanvasElement>();
	let phase = $state<'idle' | 'drawing' | 'ready' | 'error'>('idle');
	let drawn = '';

	const options = $derived<PosterOptions>({
		title,
		...(subtitle ? { subtitle } : {}),
		siteName: site.name,
		url,
		cover: imageSources(cover, site.imageBases),
		scanHint: i18n.t('share.scan')
	});

	// 面板打开后才画海报：Canvas 与二维码模块都按需加载，不进首屏包
	$effect(() => {
		const el = canvas;
		const key = `${url}|${title}|${subtitle ?? ''}`;
		if (!open || !el || !posterOn || drawn === key || phase === 'drawing') {
			return;
		}
		phase = 'drawing';
		void import('./poster.ts')
			.then(({ drawPoster }) => drawPoster(el, options))
			.then(() => {
				drawn = key;
				phase = 'ready';
			})
			.catch(() => (phase = 'error'));
	});

	async function copy() {
		try {
			await navigator.clipboard.writeText(url);
			toasts.show(i18n.t('share.copied'), { tone: 'success' });
		} catch {
			toasts.show(i18n.t('share.failed'), { tone: 'error' });
		}
	}

	async function systemShare() {
		if (typeof navigator.share !== 'function') {
			await copy();
			return;
		}
		try {
			await navigator.share({ title, url });
		} catch (err) {
			// 用户取消不算失败
			if (!(err instanceof DOMException && err.name === 'AbortError')) {
				await copy();
			}
		}
	}

	async function download() {
		const el = canvas;
		if (!el || phase !== 'ready') {
			return;
		}
		try {
			const blob = await new Promise<Blob>((resolve, reject) => {
				el.toBlob((b) => (b ? resolve(b) : reject(new Error('导出失败'))), 'image/png');
			});
			const href = URL.createObjectURL(blob);
			const link = document.createElement('a');
			link.href = href;
			link.download = posterFileName(title, subtitle);
			link.click();
			URL.revokeObjectURL(href);
			toasts.show(i18n.t('share.downloaded'), { tone: 'success' });
		} catch {
			toasts.show(i18n.t('share.posterFailed'), { tone: 'error' });
		}
	}
</script>

<Sheet center {open} title={i18n.t('share.title')} {onclose}>
	<div class="flex flex-col gap-4 px-4 pt-1 pb-4">
		{#if posterOn}
			<!-- 海报预览：固定 9:16 占位，画好前后不跳动 -->
			<div class="relative mx-auto w-full max-w-56 overflow-hidden rounded-card bg-sunken" style:aspect-ratio="{POSTER_WIDTH} / {POSTER_HEIGHT}">
				<canvas
					bind:this={canvas}
					width={POSTER_WIDTH}
					height={POSTER_HEIGHT}
					class="size-full transition-opacity duration-300 {phase === 'ready' ? 'opacity-100' : 'opacity-0'}"
					aria-label={i18n.t('share.poster')}
				></canvas>
				{#if phase !== 'ready'}
					<div class="absolute inset-0 grid place-items-center" role="status">
						{#if phase === 'error'}
							<span class="px-4 text-center text-xs text-fg-subtle">{i18n.t('share.posterFailed')}</span>
						{:else}
							<Icon name="loader-circle" size={26} class="animate-spin text-fg-subtle" />
						{/if}
					</div>
				{/if}
			</div>
			<p class="text-center text-xs text-fg-subtle">{i18n.t('share.posterTip')}</p>
		{/if}

		<div class="flex flex-col gap-2">
			{#if posterOn}
				<button type="button" class="btn btn-primary w-full" disabled={phase !== 'ready'} onclick={download}>
					<Icon name="download" size={17} />
					{i18n.t('share.download')}
				</button>
			{/if}
			<button type="button" class="btn btn-ghost w-full" onclick={copy}>
				<Icon name="link" size={17} />
				{i18n.t('share.copyLink')}
			</button>
			<button type="button" class="btn btn-ghost w-full" onclick={systemShare}>
				<Icon name="share-2" size={17} />
				{i18n.t('share.system')}
			</button>
		</div>
	</div>
</Sheet>
