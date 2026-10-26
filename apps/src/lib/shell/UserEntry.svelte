<script lang="ts">
	import { page } from '$app/state';
	import { getI18n } from '#lib/i18n/context.ts';
	import { splitLocale } from '#lib/i18n/locales.ts';
	import { session } from '#lib/stores/session.svelte.ts';
	import Icon from '#lib/ui/Icon.svelte';
	import Avatar from '#lib/ui/Avatar.svelte';

	const i18n = getI18n();
	const next = $derived(splitLocale(page.url.pathname).path + page.url.search);
</script>

<!-- 三种状态等宽（min-w），登录态确定前后顶栏不跳动 -->
<div class="flex min-w-10 justify-end lg:min-w-[5.5rem]">
	{#if session.status === 'user'}
		<a
			href={i18n.path('/me')}
			class="relative inline-flex size-10 items-center justify-center rounded-full hover:bg-raised"
			aria-label={session.me?.unread
				? i18n.t('me.titleWithUnread', { n: session.me.unread })
				: i18n.t('nav.me')}
		>
			<Avatar name={session.me?.user.nickname ?? ''} size={32} />
			{#if session.me?.unread}
				<span class="absolute end-1 top-1 size-2.5 rounded-full bg-brand ring-2 ring-canvas" aria-hidden="true"></span>
			{/if}
		</a>
	{:else if session.status === 'guest'}
		<a
			href={i18n.path(`/login?next=${encodeURIComponent(next)}`)}
			class="btn btn-sm btn-ghost hidden lg:inline-flex"
			data-sveltekit-preload-data="tap"
		>
			{i18n.t('auth.login')}
		</a>
		<a href={i18n.path('/me')} class="icon-btn lg:hidden" aria-label={i18n.t('nav.me')}>
			<Icon name="circle-user" size={22} />
		</a>
	{:else}
		<span class="size-10 lg:w-[5.5rem]" aria-hidden="true"></span>
	{/if}
</div>
