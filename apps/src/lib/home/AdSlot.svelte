<script lang="ts">
	import { onMount } from 'svelte';
	import type { AdSlot, PublicAd } from '#lib/api/types.ts';
	import { getI18n } from '#lib/i18n/context.ts';
	import BlobImage from '#lib/media/BlobImage.svelte';
	import { imageSources } from '#lib/media/image.ts';
	import { safeHref } from '#lib/shell/announcement.ts';
	import { getSite } from '#lib/site.ts';
	import SandboxHtml from './SandboxHtml.svelte';

	type Props = { slot: AdSlot | string };
	let { slot }: Props = $props();

	const i18n = getI18n();
	const site = $derived(getSite());

	// 广告按设备按天频控，素材不能进共享缓存：挂载后由浏览器经加密接口取（docs/07 /api/v1/ads）
	let ad = $state<PublicAd | null>(null);
	let phase = $state<'loading' | 'ready' | 'empty'>('loading');
	let box = $state<HTMLElement>();

	async function report(kind: 'impression' | 'click') {
		if (!ad) {
			return;
		}
		const { api } = await import('#lib/api/client.ts');
		await api.post('/api/v1/ads/event', { ad_id: ad.id, kind }).catch(() => undefined);
	}

	onMount(() => {
		const controller = new AbortController();
		const device = matchMedia('(min-width: 1024px)').matches ? 'desktop' : 'mobile';
		void import('#lib/api/client.ts')
			.then(({ api }) =>
				api.get<{ list: PublicAd[] }>(
					`/api/v1/ads?slots=${encodeURIComponent(slot)}&n=1&device=${device}&locale=${encodeURIComponent(i18n.locale)}`,
					controller.signal
				)
			)
			.then((data) => {
				ad = data.list[0] ?? null;
				phase = ad ? 'ready' : 'empty';
			})
			.catch(() => {
				if (!controller.signal.aborted) {
					phase = 'empty';
				}
			});
		return () => controller.abort();
	});

	// 曝光：至少一半可见且持续 1 秒才算一次（docs/04 C.5）
	$effect(() => {
		const el = box;
		if (!el || phase !== 'ready') {
			return;
		}
		let timer: ReturnType<typeof setTimeout> | undefined;
		const observer = new IntersectionObserver(
			([entry]) => {
				clearTimeout(timer);
				if (entry?.isIntersecting) {
					timer = setTimeout(() => {
						observer.disconnect();
						void report('impression');
					}, 1000);
				}
			},
			{ threshold: 0.5 }
		);
		observer.observe(el);
		return () => {
			clearTimeout(timer);
			observer.disconnect();
		};
	});

	const target = $derived(ad ? safeHref(ad.click_url) : null);
	const image = $derived(ad ? imageSources(ad.image, site.imageBases) : []);
</script>

{#if phase !== 'empty'}
	<aside bind:this={box} class="relative" aria-label={i18n.t('ad.label')}>
		{#if phase === 'loading'}
			<div class="skeleton aspect-[3/1] w-full rounded-2xl lg:aspect-[6/1]"></div>
		{:else if ad}
			{#if ad.creative_type === 'html' && ad.html}
				<SandboxHtml html={ad.html} title={ad.alt || i18n.t('ad.label')} minHeight={80} />
			{:else if ad.creative_type === 'text'}
				<a
					href={target ? (target.external ? target.href : i18n.path(target.href)) : undefined}
					target={target?.external ? '_blank' : undefined}
					rel={target?.external ? 'noopener noreferrer sponsored' : undefined}
					class="flex items-center gap-3 rounded-panel border border-line bg-surface px-4 py-3 text-sm text-fg hover:border-line-strong"
					onclick={() => void report('click')}
				>
					<span class="rounded bg-raised px-1.5 py-0.5 text-[11px] text-fg-subtle">{i18n.t('ad.badge')}</span>
					<span class="min-w-0 flex-1 truncate">{ad.text}</span>
				</a>
			{:else}
				<a
					href={target ? (target.external ? target.href : i18n.path(target.href)) : undefined}
					target={target?.external ? '_blank' : undefined}
					rel={target?.external ? 'noopener noreferrer sponsored' : undefined}
					class="relative block overflow-hidden rounded-2xl ring-1 ring-line ring-inset"
					onclick={() => void report('click')}
				>
					<BlobImage src={image} alt={ad.alt || ad.text || i18n.t('ad.label')} width={1200} height={300} class="aspect-[3/1]! lg:aspect-[6/1]!" />
					<span class="absolute end-2 top-2 rounded bg-black/55 px-1.5 py-0.5 text-[10px] text-white/90 backdrop-blur">
						{i18n.t('ad.badge')}
					</span>
				</a>
			{/if}
		{/if}
	</aside>
{/if}
