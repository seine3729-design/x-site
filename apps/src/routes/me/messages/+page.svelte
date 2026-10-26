<script lang="ts">
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import type { Notification } from '#lib/api/types.ts';
	import { relativeTime } from '#lib/format.ts';
	import { getI18n } from '#lib/i18n/context.ts';
	import SubHeader from '#lib/me/SubHeader.svelte';
	import { session } from '#lib/stores/session.svelte.ts';
	import { toasts } from '#lib/stores/toast.svelte.ts';
	import EmptyState from '#lib/ui/EmptyState.svelte';
	import Icon from '#lib/ui/Icon.svelte';
	import Skeletons from '#lib/ui/Skeletons.svelte';

	const i18n = getI18n();
	let items = $state<Notification[] | null>(null);

	$effect(() => {
		if (session.status === 'guest') {
			void goto(i18n.path('/login?next=%2Fme%2Fmessages'), { replace: true });
			return;
		}
		if (session.status !== 'user') {
			return;
		}
		void import('#lib/api/client.ts')
			.then(({ api }) => api.get<{ items: Notification[] }>('/api/v1/me/notifications?limit=50'))
			.then((data) => (items = data.items))
			.catch(() => (items = []));
	});

	async function readAll() {
		try {
			const { api } = await import('#lib/api/client.ts');
			await api.post('/api/v1/me/notifications', {});
			items = (items ?? []).map((n) => ({ ...n, is_read: true }));
			session.setUnread(0);
		} catch {
			toasts.show(i18n.error('NETWORK'), { tone: 'error' });
		}
	}

	const ICON = { reply: 'message-circle', like: 'thumbs-up', update: 'bell' } as const;
	const TYPE = { reply: 'messages.reply', like: 'messages.like', update: 'messages.update' } as const;
	const text = (n: Notification) => {
		const value = n.payload['text'] ?? n.payload['title'] ?? n.payload['content'];
		return typeof value === 'string' ? value : '';
	};
</script>

<svelte:head>
	<title>{i18n.t('me.messages')} - {page.data.site.name}</title>
	<meta name="robots" content="noindex" />
</svelte:head>

<div class="page-x max-w-3xl! pt-4 lg:pt-8">
	<SubHeader title={i18n.t('me.messages')}>
		{#snippet actions()}
			{#if items?.some((n) => !n.is_read)}
				<button type="button" class="btn btn-sm btn-ghost" onclick={readAll}>
					<Icon name="check" size={15} />{i18n.t('messages.readAll')}
				</button>
			{/if}
		{/snippet}
	</SubHeader>

	{#if items === null}
		<Skeletons kind="list" count={4} />
	{:else if items.length === 0}
		<EmptyState icon="bell" title={i18n.t('messages.empty')} description={i18n.t('messages.emptyDesc')} />
	{:else}
		<ul class="flex flex-col gap-2">
			{#each items as item (item.id)}
				<li class="flex gap-3 rounded-panel border border-line bg-surface p-4 {item.is_read ? '' : 'border-brand-fg/30'}">
					<span class="relative grid size-10 shrink-0 place-items-center rounded-full bg-raised text-fg-muted">
						<Icon name={ICON[item.type]} size={18} />
						{#if !item.is_read}<span class="absolute end-0 top-0 size-2.5 rounded-full bg-brand ring-2 ring-surface" aria-hidden="true"></span>{/if}
					</span>
					<span class="min-w-0 flex-1">
						<span class="block text-sm font-semibold">
							{i18n.t(TYPE[item.type])}
							{#if !item.is_read}<span class="sr-only">{i18n.t('messages.unread')}</span>{/if}
						</span>
						{#if text(item)}<span class="mt-0.5 block text-sm text-fg-muted">{text(item)}</span>{/if}
						<span class="mt-1 block text-xs text-fg-subtle">{relativeTime(item.created_at * 1000, i18n.locale)}</span>
					</span>
				</li>
			{/each}
		</ul>
	{/if}
</div>
