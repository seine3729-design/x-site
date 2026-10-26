<script lang="ts">
	import { page } from '$app/state';
	import { onMount } from 'svelte';
	import type { Announcement } from '#lib/api/types.ts';
	import { getI18n } from '#lib/i18n/context.ts';
	import BlobImage from '#lib/media/BlobImage.svelte';
	import { imageSources } from '#lib/media/image.ts';
	import { announcementPage } from '#lib/nav.ts';
	import { getSite } from '#lib/site.ts';
	import Icon from '#lib/ui/Icon.svelte';
	import Modal from '#lib/ui/Modal.svelte';
	import { markSeen, safeHref, shouldShow } from './announcement.ts';

	const i18n = getI18n();
	const site = $derived(getSite());
	const pageKey = $derived(announcementPage(page.route.id));

	// 显示与否取决于本机记录，只能在浏览器里判断；SSR 不输出公告，避免与本机记录不一致时闪一下
	let mounted = $state(false);
	let dismissed = $state<number[]>([]);
	onMount(() => {
		mounted = true;
	});

	const visible = $derived(
		mounted
			? site.announcements.filter((item) => !dismissed.includes(item.id) && shouldShow(item, pageKey))
			: []
	);
	const topbar = $derived(visible.find((item) => item.kind === 'topbar'));
	const popup = $derived(visible.find((item) => item.kind === 'popup'));

	function close(item: Announcement) {
		markSeen(item);
		dismissed = [...dismissed, item.id];
	}

	const link = (item: Announcement) => {
		const target = safeHref(item.click_url);
		return target ? { ...target, href: target.external ? target.href : i18n.path(target.href) } : null;
	};
</script>

{#if topbar}
	{@const target = link(topbar)}
	<div class="border-b border-line bg-gradient-to-r from-brand-soft via-transparent to-brand-soft">
		<div class="page-x flex h-10 items-center gap-3 text-[13px]">
			<Icon name="megaphone" size={16} class="text-brand-fg" />
			{#if target}
				<a
					href={target.href}
					target={target.external ? '_blank' : undefined}
					rel={target.external ? 'noopener noreferrer' : undefined}
					class="min-w-0 flex-1 truncate text-fg hover:underline"
					title={topbar.content}
				>
					{topbar.content}
				</a>
			{:else}
				<p class="min-w-0 flex-1 truncate text-fg" title={topbar.content}>{topbar.content}</p>
			{/if}
			<button type="button" class="icon-btn size-8" aria-label={i18n.t('common.close')} onclick={() => close(topbar)}>
				<Icon name="x" size={16} />
			</button>
		</div>
	</div>
{/if}

{#if popup}
	{@const target = link(popup)}
	{@const image = imageSources(popup.image, site.imageBases)}
	<Modal open label={i18n.t('announce.title')} onclose={() => close(popup)} size="sm">
		{#if image.length > 0}
			<BlobImage src={image} alt="" width={640} height={360} class="-mx-6 -mt-6 mb-5 rounded-t-2xl" />
		{/if}
		<h2 class="mb-2 flex items-center gap-2 text-lg font-bold">
			<Icon name="megaphone" size={20} class="text-brand-fg" />
			{i18n.t('announce.title')}
		</h2>
		<p class="text-[15px] leading-7 whitespace-pre-line text-fg-muted">{popup.content}</p>
		<div class="mt-6 flex justify-end gap-3">
			<button type="button" class="btn btn-ghost" onclick={() => close(popup)}>{i18n.t('common.gotIt')}</button>
			{#if target}
				<a
					href={target.href}
					target={target.external ? '_blank' : undefined}
					rel={target.external ? 'noopener noreferrer' : undefined}
					class="btn btn-primary"
					onclick={() => close(popup)}
				>
					{i18n.t('common.viewDetail')}
				</a>
			{/if}
		</div>
	</Modal>
{/if}
