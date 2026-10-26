<script lang="ts">
	import { page } from '$app/state';
	import Badge from '#lib/drama/Badge.svelte';
	import { dramaPath, episodesLabel, playPath } from '#lib/drama/labels.ts';
	import Poster from '#lib/drama/Poster.svelte';
	import { compactNumber, relativeTime, score } from '#lib/format.ts';
	import { getI18n } from '#lib/i18n/context.ts';
	import EmptyState from '#lib/ui/EmptyState.svelte';
	import Icon from '#lib/ui/Icon.svelte';

	let { data } = $props();
	const i18n = getI18n();
	const list = $derived(data.rank.list);
	const podium = $derived(list.slice(0, 3));
	const rest = $derived(list.slice(3));
	/** 热度条按榜首归一化：热度分本身是内部计分，给用户看相对高低比看绝对值有意义 */
	const maxHeat = $derived(Math.max(1, ...list.map((item) => item.heat)));
	// TOP1 金、TOP2 银、TOP3 铜
	const MEDAL = [
		'bg-gradient-to-br from-[#ffe08a] to-[#e3a93a] text-[#3d2a05]',
		'bg-gradient-to-br from-[#f1f1f4] to-[#a8a8b3] text-[#26262c]',
		'bg-gradient-to-br from-[#f3b98a] to-[#b8693a] text-[#2f1606]'
	];
	const tabs = $derived([
		{ board: 'drama_week', key: 'rank.week' as const, href: i18n.path('/rank') },
		{ board: 'drama_month', key: 'rank.month' as const, href: i18n.path('/rank?board=drama_month') }
	]);
</script>

<svelte:head>
	<title>{i18n.t(data.board === 'drama_month' ? 'rank.month' : 'rank.week')} - {page.data.site.name}</title>
	<meta name="description" content={i18n.t('rank.description')} />
</svelte:head>

<div class="relative isolate overflow-hidden">
	<div
		class="absolute inset-x-0 top-0 -z-10 h-72 bg-[radial-gradient(60%_100%_at_50%_0%,rgb(225_29_72/0.28),transparent)]"
		aria-hidden="true"
	></div>
	<div class="page-x max-w-6xl! pt-6 pb-10 lg:pt-10">
		<h1 class="flex items-center gap-2 text-2xl font-extrabold tracking-tight lg:text-3xl">
			<Icon name="trophy" size={28} class="text-vip-fg" />
			{i18n.t('rank.title')}
		</h1>
		<p class="mt-1.5 text-[13px] leading-5 text-fg-subtle">
			{i18n.t('rank.description')}
			{#if data.rank.computed_at}
				<span class="ms-1">{i18n.t('rank.updatedAt', { time: relativeTime(data.rank.computed_at, i18n.locale) })}</span>
			{/if}
		</p>

		<!-- 榜单切换放标题下方，当前项用品牌色实心胶囊 -->
		<nav class="mt-4 mb-6 flex gap-2" aria-label={i18n.t('rank.boards')}>
			{#each tabs as tab (tab.board)}
				<a
					href={tab.href}
					aria-current={data.board === tab.board ? 'page' : undefined}
					data-sveltekit-noscroll
					class="rounded-full px-5 py-2 text-sm font-semibold transition-colors aria-[current=page]:bg-brand aria-[current=page]:text-on-brand aria-[current=page]:shadow-[0_8px_20px_-8px_rgb(0_0_0/0.45)] {data.board ===
					tab.board
						? ''
						: 'bg-sunken text-fg-muted hover:text-fg'}">{i18n.t(tab.key)}</a
				>
			{/each}
		</nav>

		{#if data.disabled || list.length === 0}
			<EmptyState icon="trophy" title={i18n.t(data.disabled ? 'rank.disabled' : 'rank.empty')}>
				<a href={i18n.path('/explore')} class="btn btn-ghost">{i18n.t('home.goExplore')}</a>
			</EmptyState>
		{:else}
			<!-- 前三名：横向大卡（海报 + 信息 + 热度条 + 立即观看），TOP1 用品牌色描边突出 -->
			<ol class="mb-7 grid gap-3 md:grid-cols-3 md:gap-4">
				{#each podium as item, index (item.drama.id)}
					{@const rating = score(item.drama.score)}
					<li
						class="group rounded-panel border p-3 transition-colors {index === 0
							? 'border-brand-fg/45 bg-brand-soft/20'
							: 'border-line bg-surface hover:border-line-strong'}"
					>
						<div class="flex gap-3">
							<a
								href={i18n.path(dramaPath(item.drama.id))}
								class="relative w-20 shrink-0 overflow-hidden rounded-card ring-1 ring-line ring-inset md:w-24"
								tabindex="-1"
								aria-hidden="true"
							>
								<Poster
									cover={item.drama.cover_portrait}
									title={item.drama.title}
									priority
									imgClass="transition-transform duration-500 group-hover:scale-[1.04]"
								/>
								<!-- TOP1 这类拉丁字标各语言都一样，当装饰字形处理；读屏听到的是下面本地化的「第 N 名」 -->
								<span class="absolute start-0 top-0 rounded-ee-lg px-1.5 py-0.5 text-[10px] leading-4 font-black {MEDAL[index]}" aria-hidden="true">
									TOP{item.rank}
								</span>
							</a>

							<div class="flex min-w-0 flex-1 flex-col">
								<h2 class="line-clamp-2 text-[15px] leading-snug font-bold lg:text-base">
									<a href={i18n.path(dramaPath(item.drama.id))} class="transition-colors hover:text-brand-fg">{item.drama.title}</a>
								</h2>
								<span class="sr-only">{i18n.t('rank.position', { n: item.rank })}</span>
								<p class="mt-1.5 flex flex-wrap items-center gap-x-2 gap-y-1 text-[11px] leading-4 text-fg-muted">
									{#if rating}
										<span class="flex items-center gap-0.5 font-bold text-vip-fg">
											<Icon name="star" size={10} filled strokeWidth={0} />{rating}
										</span>
									{/if}
									<span>{episodesLabel(i18n, item.drama)}</span>
									{#if item.drama.year}<span class="tabular">{item.drama.year}</span>{/if}
									{#if item.drama.play_count > 0}
										<span class="tabular">{i18n.t('drama.plays', { n: compactNumber(item.drama.play_count, i18n.locale) })}</span>
									{/if}
								</p>
								<Badge value={item.drama.badge} class="mt-2" />

								<div class="mt-auto pt-2.5">
									<span class="block h-1 overflow-hidden rounded-full bg-raised" aria-hidden="true">
										<span
											class="block h-full rounded-full bg-gradient-to-r from-brand to-[#ff8f6b]"
											style:width="{Math.max(8, Math.round((item.heat / maxHeat) * 100))}%"
										></span>
									</span>
									<div class="mt-2 flex items-center justify-between gap-2">
										<span class="flex items-center gap-1 text-xs font-bold text-brand-fg">
											<Icon name="flame" size={14} />
											<span class="text-fg-subtle">{i18n.t('rank.heat')}</span>
											<span class="tabular">{compactNumber(item.heat, i18n.locale)}</span>
										</span>
										{#if item.drama.latest_episode > 0}
											<a href={i18n.path(playPath(item.drama.id, 1))} class="btn btn-sm btn-primary gap-1">
												<Icon name="play" size={13} filled strokeWidth={0} />
												{i18n.t('rank.watch')}
											</a>
										{/if}
									</div>
								</div>
							</div>
						</div>
					</li>
				{/each}
			</ol>

			<!-- 第 4 名起：电脑端两列，一屏能看到更多名次 -->
			<ol class="grid border-t border-line md:grid-cols-2 md:gap-x-8" start="4">
				{#each rest as item (item.drama.id)}
					<li class="border-b border-line">
						<div class="flex items-center gap-2.5 py-2.5 md:gap-3">
							<span class="w-6 shrink-0 text-center text-base font-black text-fg-subtle italic tabular md:text-lg">{item.rank}</span>
							<a
								href={i18n.path(dramaPath(item.drama.id))}
								class="w-14 shrink-0 overflow-hidden rounded-card ring-1 ring-line ring-inset md:w-16"
								tabindex="-1"
								aria-hidden="true"
							>
								<Poster cover={item.drama.cover_portrait} title={item.drama.title} />
							</a>
							<div class="min-w-0 flex-1">
								<!-- py-px：手机上这是列表的主入口，可点高度要够 24px（WCAG 2.5.8） -->
								<a
									href={i18n.path(dramaPath(item.drama.id))}
									class="line-clamp-1 py-px text-sm font-semibold transition-colors hover:text-brand-fg">{item.drama.title}</a
								>
								<!-- 这里只放集数与热度：热度分和播放量在本地库里数量级相同，两个数字并排看着像重复 -->
								<p class="mt-0.5 flex flex-wrap items-center gap-x-2 text-[11px] leading-4 text-fg-subtle">
									<span>{episodesLabel(i18n, item.drama)}</span>
									<span class="flex items-center gap-0.5 font-semibold text-brand-fg">
										<Icon name="flame" size={11} />
										<span>{i18n.t('rank.heat')}</span>
										<span class="tabular">{compactNumber(item.heat, i18n.locale)}</span>
									</span>
								</p>
							</div>
							{#if item.drama.latest_episode > 0}
								<a
									href={i18n.path(playPath(item.drama.id, 1))}
									class="btn btn-sm shrink-0 border border-brand-fg/45 bg-transparent px-3.5 text-brand-fg hover:bg-brand-soft"
								>
									{i18n.t('rank.watch')}
								</a>
							{/if}
						</div>
					</li>
				{/each}
			</ol>
		{/if}
	</div>
</div>
