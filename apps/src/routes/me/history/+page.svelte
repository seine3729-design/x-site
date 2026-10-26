<script lang="ts">
	import { page } from '$app/state';
	import { playPath } from '#lib/drama/labels.ts';
	import Poster from '#lib/drama/Poster.svelte';
	import { clock, relativeTime, startOfDay } from '#lib/format.ts';
	import { getI18n } from '#lib/i18n/context.ts';
	import { deleteHistory, loadHistory, progressOf, type HistoryRow } from '#lib/me/history.ts';
	import SubHeader from '#lib/me/SubHeader.svelte';
	import { session } from '#lib/stores/session.svelte.ts';
	import { toasts } from '#lib/stores/toast.svelte.ts';
	import EmptyState from '#lib/ui/EmptyState.svelte';
	import Icon from '#lib/ui/Icon.svelte';
	import Skeletons from '#lib/ui/Skeletons.svelte';

	const i18n = getI18n();
	let rows = $state<HistoryRow[] | null>(null);
	let failed = $state(false);
	let editing = $state(false);
	let selected = $state<number[]>([]);
	/** 清空后 5 秒内可撤销：先从界面隐藏，到点才真正删除（docs/04 B.11） */
	let pendingClear: ReturnType<typeof setTimeout> | null = null;
	let hidden = $state(false);

	$effect(() => {
		if (session.status === 'unknown') {
			return;
		}
		const loggedIn = session.loggedIn;
		failed = false;
		loadHistory(loggedIn)
			.then((list) => (rows = list))
			.catch(() => {
				failed = true;
				rows = [];
			});
	});

	const visible = $derived(hidden ? [] : (rows ?? []));

	// 按「今天 / 昨天 / 更早」分组
	const groups = $derived.by(() => {
		const startOfToday = startOfDay(Date.now());
		const out: { key: 'history.today' | 'history.yesterday' | 'history.earlier'; rows: HistoryRow[] }[] = [
			{ key: 'history.today', rows: [] },
			{ key: 'history.yesterday', rows: [] },
			{ key: 'history.earlier', rows: [] }
		];
		for (const row of visible) {
			const index = row.updatedMs >= startOfToday ? 0 : row.updatedMs >= startOfToday - 86_400_000 ? 1 : 2;
			out[index]!.rows.push(row);
		}
		return out.filter((g) => g.rows.length > 0);
	});

	async function removeSelected() {
		const ids = [...selected];
		if (ids.length === 0) {
			return;
		}
		const before = rows;
		rows = (rows ?? []).filter((r) => !ids.includes(r.drama_id));
		selected = [];
		editing = false;
		try {
			await deleteHistory(session.loggedIn, ids);
		} catch {
			rows = before;
			toasts.show(i18n.error('NETWORK'), { tone: 'error' });
		}
	}

	function clearAll() {
		hidden = true;
		editing = false;
		pendingClear = setTimeout(async () => {
			pendingClear = null;
			try {
				await deleteHistory(session.loggedIn, []);
				rows = [];
			} catch {
				toasts.show(i18n.error('NETWORK'), { tone: 'error' });
			} finally {
				hidden = false;
			}
		}, 5000);
		toasts.show(i18n.t('history.cleared'), {
			duration: 5000,
			action: {
				label: i18n.t('common.undo'),
				run: () => {
					if (pendingClear) {
						clearTimeout(pendingClear);
						pendingClear = null;
					}
					hidden = false;
				}
			}
		});
	}

	function toggle(id: number) {
		selected = selected.includes(id) ? selected.filter((v) => v !== id) : [...selected, id];
	}
</script>

<svelte:head>
	<title>{i18n.t('me.history')} - {page.data.site.name}</title>
	<meta name="robots" content="noindex" />
</svelte:head>

<div class="page-x max-w-4xl! pt-4 lg:pt-8">
	<SubHeader title={i18n.t('me.history')}>
		{#snippet actions()}
			{#if visible.length > 0}
				{#if editing}
					<button type="button" class="btn btn-sm btn-ghost" onclick={() => ((editing = false), (selected = []))}>{i18n.t('common.done')}</button>
				{:else}
					<button type="button" class="btn btn-sm btn-ghost" onclick={() => (editing = true)}>
						<Icon name="square-pen" size={15} />{i18n.t('common.edit')}
					</button>
				{/if}
			{/if}
		{/snippet}
	</SubHeader>

	{#if session.status === 'guest'}
		<p class="mb-5 flex items-center gap-2 rounded-2xl bg-raised px-4 py-3 text-[13px] text-fg-muted">
			<Icon name="info" size={16} class="shrink-0 text-brand-fg" />
			<span class="flex-1">{i18n.t('history.localNote')}</span>
			<a href={i18n.path('/login?next=%2Fme%2Fhistory')} class="shrink-0 font-semibold text-brand-fg hover:underline">{i18n.t('auth.login')}</a>
		</p>
	{/if}

	{#if rows === null}
		<Skeletons kind="list" count={5} />
	{:else if visible.length === 0}
		<EmptyState icon="history" title={i18n.t(failed ? 'common.loadFailed' : 'history.empty')} description={failed ? '' : i18n.t('history.emptyDesc')}>
			<a href={i18n.path('/')} class="btn btn-primary">{i18n.t('history.goWatch')}</a>
		</EmptyState>
	{:else}
		<div class="flex flex-col gap-7">
			{#each groups as group (group.key)}
				<section>
					<h2 class="mb-3 text-sm font-semibold text-fg-subtle">{i18n.t(group.key)}</h2>
					<ul class="flex flex-col gap-2">
						{#each group.rows as row (row.drama_id)}
							{@const pct = Math.round(progressOf(row) * 100)}
							<li class="flex items-center gap-3">
								{#if editing}
									<input
										type="checkbox"
										class="size-5 shrink-0 accent-[var(--brand)]"
										checked={selected.includes(row.drama_id)}
										aria-label={i18n.t('history.select', { title: row.title })}
										onchange={() => toggle(row.drama_id)}
									/>
								{/if}
								<a
									href={i18n.path(playPath(row.drama_id, row.episode_number))}
									class="flex min-w-0 flex-1 items-center gap-3 rounded-panel border border-line bg-surface p-2.5 transition-colors hover:border-line-strong"
								>
									<span class="relative w-16 shrink-0 overflow-hidden rounded-xl sm:w-20">
										<Poster cover={row.cover} title={row.title} />
										<span class="absolute inset-x-0 bottom-0 h-1 bg-black/40"><span class="block h-full bg-brand" style:width="{pct}%"></span></span>
									</span>
									<span class="min-w-0 flex-1">
										<span class="line-clamp-1 text-[15px] font-semibold">{row.title}</span>
										<span class="mt-1 block text-[13px] text-fg-muted">
											{i18n.t('me.watchedTo', { n: row.episode_number })} · {clock(row.position_sec)}
										</span>
										<span class="mt-0.5 block text-xs text-fg-subtle">{relativeTime(row.updatedMs, i18n.locale)}</span>
									</span>
									<span class="me-1 hidden size-10 shrink-0 place-items-center rounded-full bg-brand-soft text-brand-fg sm:grid">
										<Icon name="play" size={16} filled strokeWidth={0} class="translate-x-px" />
									</span>
								</a>
							</li>
						{/each}
					</ul>
				</section>
			{/each}
		</div>

		{#if editing}
			<div class="sticky bottom-[calc(var(--tabbar-h)+var(--safe-bottom)+0.75rem)] mt-6 flex gap-3 lg:bottom-6">
				<button type="button" class="btn btn-ghost flex-1" onclick={clearAll}>
					<Icon name="trash-2" size={16} />{i18n.t('history.clearAll')}
				</button>
				<button type="button" class="btn btn-primary flex-1" disabled={selected.length === 0} onclick={removeSelected}>
					{i18n.t('history.deleteSelected', { n: selected.length })}
				</button>
			</div>
		{/if}
	{/if}
</div>
