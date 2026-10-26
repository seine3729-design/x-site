<script lang="ts">
	import { page } from '$app/state';
	import { getI18n } from '#lib/i18n/context.ts';
	import { splitLocale } from '#lib/i18n/locales.ts';
	import Icon from '#lib/ui/Icon.svelte';
	import type { Wall } from './controller.svelte.ts';

	type Props = { wall: Wall; trialEnded: boolean; onretry: () => void; detailHref: string };
	let { wall, trialEnded, onretry, detailHref }: Props = $props();

	const i18n = getI18n();
	const next = $derived(encodeURIComponent(splitLocale(page.url.pathname).path + page.url.search));
</script>

<!-- 观看权限与故障提示（docs/04 B.6.5）：盖在画面上，按钮都能用键盘操作 -->
<div class="absolute inset-0 z-20 grid place-items-center bg-black/70 p-6 text-center text-white backdrop-blur-md" role="alert">
	<div class="flex max-w-xs animate-rise flex-col items-center">
		{#if wall === 'login'}
			<span class="mb-4 grid size-14 place-items-center rounded-2xl bg-white/12"><Icon name="lock" size={26} /></span>
			<p class="text-lg font-bold">{i18n.t(trialEnded ? 'player.trialEndLogin' : 'player.loginTitle')}</p>
			<p class="mt-2 text-sm leading-6 text-white/75">{i18n.t('player.loginDesc')}</p>
			<div class="mt-6 flex w-full flex-col gap-3">
				<a href={i18n.path(`/login?next=${next}`)} class="btn btn-primary w-full">{i18n.t('auth.login')}</a>
				<a href={i18n.path(`/register?next=${next}`)} class="btn btn-glass w-full">{i18n.t('auth.registerFree')}</a>
			</div>
		{:else if wall === 'vip'}
			<span class="mb-4 grid size-14 place-items-center rounded-2xl bg-gradient-to-br from-[#f7d27a] to-[#e3a93a] text-on-vip">
				<Icon name="crown" size={26} />
			</span>
			<p class="text-lg font-bold">{i18n.t(trialEnded ? 'player.trialEndVip' : 'player.vipTitle')}</p>
			<p class="mt-2 text-sm leading-6 text-white/75">{i18n.t('player.vipDesc')}</p>
			<div class="mt-6 flex w-full flex-col gap-3">
				<a href={i18n.path(`/vip?next=${next}`)} class="btn btn-vip w-full">
					<Icon name="crown" size={17} />
					{i18n.t('vip.open')}
				</a>
				<a href={detailHref} class="btn btn-glass w-full">{i18n.t('player.backToDetail')}</a>
			</div>
		{:else if wall === 'unsupported'}
			<span class="mb-4 grid size-14 place-items-center rounded-2xl bg-white/12"><Icon name="smartphone" size={26} /></span>
			<p class="text-base leading-7">{i18n.t('player.unsupported')}</p>
		{:else if wall === 'notfound'}
			<span class="mb-4 grid size-14 place-items-center rounded-2xl bg-white/12"><Icon name="clock" size={26} /></span>
			<p class="text-lg font-bold">{i18n.t('player.notReady')}</p>
			<a href={detailHref} class="btn btn-glass mt-6">{i18n.t('player.backToDetail')}</a>
		{:else}
			<span class="mb-4 grid size-14 place-items-center rounded-2xl bg-white/12">
				<Icon name={wall === 'unavailable' ? 'wifi-off' : 'triangle-alert'} size={26} />
			</span>
			<p class="text-lg font-bold">
				{i18n.t(wall === 'unavailable' ? 'player.unavailable' : wall === 'ratelimited' ? 'error.RATE_LIMITED' : 'player.error')}
			</p>
			<p class="mt-2 text-sm leading-6 text-white/75">{i18n.t('player.switchHint')}</p>
			<button type="button" class="btn btn-primary mt-6" onclick={onretry}>
				<Icon name="refresh-cw" size={17} />
				{i18n.t('common.retry')}
			</button>
		{/if}
	</div>
</div>
