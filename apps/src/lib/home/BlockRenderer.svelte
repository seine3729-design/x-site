<script lang="ts">
	import type { HomeBlock } from '#lib/api/types.ts';
	import DramaCard from '#lib/drama/DramaCard.svelte';
	import DramaGrid from '#lib/drama/DramaGrid.svelte';
	import { getI18n } from '#lib/i18n/context.ts';
	import { safeHref } from '#lib/shell/announcement.ts';
	import { getSite } from '#lib/site.ts';
	import Icon from '#lib/ui/Icon.svelte';
	import Rail from '#lib/ui/Rail.svelte';
	import SectionHeader from '#lib/ui/SectionHeader.svelte';
	import AdSlot from './AdSlot.svelte';
	import FeatureCards from './FeatureCards.svelte';
	import HeroCarousel from './HeroCarousel.svelte';
	import RankRail from './RankRail.svelte';
	import SandboxHtml from './SandboxHtml.svelte';
	import TagCloud from './TagCloud.svelte';
	import TopicCards from './TopicCards.svelte';

	type Props = { block: HomeBlock; index: number };
	let { block, index }: Props = $props();

	const i18n = getI18n();
	const site = $derived(getSite());
	const dramas = $derived(block.dramas ?? []);
	// 首屏前两个区块的图片立即加载
	const eager = $derived(index < 2);
	const titleId = $derived(`block-${block.id}`);

	const rankTitle = $derived(
		block.title || i18n.t(block.rank_board === 'drama_month' ? 'rank.month' : 'rank.week')
	);
	const rankMore = $derived(
		block.more_link || `/rank${block.rank_board === 'drama_month' ? '?board=drama_month' : ''}`
	);
	const announcement = $derived(
		block.type === 'announce'
			? site.announcements.find((a) => !block.announce_kind || a.kind === block.announce_kind)
			: undefined
	);
</script>

{#if block.type === 'carousel' && dramas.length > 0}
	<HeroCarousel {dramas} label={block.title || i18n.t('home.featured')} priority={eager} />
{:else if block.type === 'feature' && dramas.length > 0}
	<section aria-labelledby={titleId}>
		<SectionHeader id={titleId} title={block.title || i18n.t('home.featured')} subtitle={block.subtitle} more={block.more_link} />
		<FeatureCards {dramas} />
	</section>
{:else if block.type === 'poster_grid' && dramas.length > 0}
	<section aria-labelledby={titleId}>
		<SectionHeader id={titleId} title={block.title} subtitle={block.subtitle} more={block.more_link} />
		<DramaGrid cards={dramas} cols={block.cols_desktop} priorityCount={eager ? 6 : 0} />
	</section>
{:else if block.type === 'h_scroll' && dramas.length > 0}
	<section aria-labelledby={titleId}>
		<SectionHeader id={titleId} title={block.title} subtitle={block.subtitle} more={block.more_link} />
		<Rail items={dramas} key={(d) => d.id} label={block.title}>
			{#snippet item(card, i)}
				<DramaCard {card} priority={eager && i < 4} />
			{/snippet}
		</Rail>
	</section>
{:else if block.type === 'rank' && dramas.length > 0}
	<section aria-labelledby={titleId}>
		<SectionHeader id={titleId} title={rankTitle} subtitle={block.subtitle} more={rankMore} />
		<RankRail {dramas} label={rankTitle} />
	</section>
{:else if block.type === 'topic_group' && (block.topics?.length ?? 0) > 0}
	<section aria-labelledby={titleId}>
		<SectionHeader id={titleId} title={block.title || i18n.t('nav.topics')} subtitle={block.subtitle} more={block.more_link || '/topics'} />
		<TopicCards topics={block.topics ?? []} />
	</section>
{:else if block.type === 'tag_cloud' && (block.tags?.length ?? 0) > 0}
	<section aria-labelledby={titleId}>
		<SectionHeader id={titleId} title={block.title || i18n.t('home.tags')} subtitle={block.subtitle} more={block.more_link} />
		<TagCloud tags={block.tags ?? []} />
	</section>
{:else if block.type === 'ad' && block.ad_slot}
	<AdSlot slot={block.ad_slot} />
{:else if block.type === 'announce' && announcement}
	{@const target = safeHref(announcement.click_url)}
	<div class="flex items-center gap-3 rounded-panel border border-line bg-surface px-4 py-3 text-sm">
		<span class="grid size-8 shrink-0 place-items-center rounded-full bg-brand-soft text-brand-fg">
			<Icon name="megaphone" size={16} />
		</span>
		<p class="min-w-0 flex-1 text-fg">{announcement.content}</p>
		{#if target}
			<a
				href={target.external ? target.href : i18n.path(target.href)}
				target={target.external ? '_blank' : undefined}
				rel={target.external ? 'noopener noreferrer' : undefined}
				class="shrink-0 font-semibold text-brand-fg hover:underline">{i18n.t('common.viewDetail')}</a
			>
		{/if}
	</div>
{:else if block.type === 'custom_html' && block.html}
	<section aria-label={block.title || i18n.t('home.custom')}>
		{#if block.title}
			<SectionHeader title={block.title} subtitle={block.subtitle} more={block.more_link} />
		{/if}
		<SandboxHtml html={block.html} title={block.title || i18n.t('home.custom')} />
	</section>
{/if}
