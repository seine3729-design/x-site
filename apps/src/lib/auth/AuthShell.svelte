<script lang="ts">
	import type { Snippet } from 'svelte';
	import { getI18n } from '#lib/i18n/context.ts';
	import Logo from '#lib/shell/Logo.svelte';
	import Icon from '#lib/ui/Icon.svelte';

	type Props = { title: string; subtitle: string; children: Snippet; footer?: Snippet };
	let { title, subtitle, children, footer }: Props = $props();

	const i18n = getI18n();
	const BENEFITS = [
		{ icon: 'history', key: 'auth.benefitSync' },
		{ icon: 'bell', key: 'auth.benefitUpdate' },
		{ icon: 'bookmark-check', key: 'auth.benefitMerge' }
	] as const;
</script>

<div class="relative isolate flex min-h-[calc(100dvh-var(--header-h))] items-start justify-center overflow-hidden px-4 py-8 lg:items-center lg:py-14">
	<div
		class="absolute inset-0 -z-10 bg-[radial-gradient(50%_60%_at_15%_10%,rgb(225_29_72/0.22),transparent),radial-gradient(40%_50%_at_90%_90%,rgb(147_51_234/0.18),transparent)]"
		aria-hidden="true"
	></div>
	<div class="grid w-full max-w-4xl overflow-hidden rounded-[1.75rem] border border-line bg-surface shadow-pop lg:grid-cols-[1fr_1.1fr]">
		<!-- 电脑端左侧：品牌与注册的好处（docs/04 C.6） -->
		<div class="relative hidden flex-col justify-between overflow-hidden bg-gradient-to-br from-[#2a0a16] via-[#1a0c1f] to-[#0d0d14] p-10 text-white lg:flex">
			<div class="absolute -end-20 -top-20 size-72 rounded-full bg-brand/30 blur-3xl" aria-hidden="true"></div>
			<Logo />
			<div class="relative">
				<p class="text-2xl leading-snug font-extrabold">{i18n.t('auth.heroTitle')}</p>
				<ul class="mt-6 space-y-4">
					{#each BENEFITS as item (item.key)}
						<li class="flex items-center gap-3 text-sm text-white/85">
							<span class="grid size-9 place-items-center rounded-xl bg-white/10"><Icon name={item.icon} size={18} /></span>
							{i18n.t(item.key)}
						</li>
					{/each}
				</ul>
			</div>
			<p class="relative text-xs text-white/50">{i18n.t('auth.heroNote')}</p>
		</div>

		<div class="p-6 sm:p-10">
			<h1 class="text-2xl font-extrabold tracking-tight">{title}</h1>
			<p class="mt-1.5 text-sm text-fg-muted">{subtitle}</p>
			<div class="mt-7">{@render children()}</div>
			{#if footer}<div class="mt-6 border-t border-line pt-5 text-center text-sm text-fg-muted">{@render footer()}</div>{/if}
		</div>
	</div>
</div>
