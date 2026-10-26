<script lang="ts">
	import { onMount } from 'svelte';
	import { getI18n } from '#lib/i18n/context.ts';
	import { getSite } from '#lib/site.ts';
	import { applyTheme, followSystem, readTheme, type ThemePreference } from '#lib/stores/theme.ts';
	import Icon from '#lib/ui/Icon.svelte';

	type Props = { variant?: 'icon' | 'segmented' };
	let { variant = 'icon' }: Props = $props();

	const i18n = getI18n();
	const site = $derived(getSite());
	// SSR 时按后台配置的默认深浅色渲染；挂载后读 <html data-theme>（首帧前已由内联脚本写好）
	let preference = $state<ThemePreference>('dark');
	let light = $state(false);

	onMount(() => {
		preference = readTheme();
		light = document.documentElement.classList.contains('light');
		return followSystem();
	});

	function choose(next: ThemePreference) {
		applyTheme(next);
		preference = next;
		light = document.documentElement.classList.contains('light');
	}

	const OPTIONS: { value: ThemePreference; icon: 'moon' | 'sun' | 'monitor'; key: 'theme.dark' | 'theme.light' | 'theme.system' }[] = [
		{ value: 'dark', icon: 'moon', key: 'theme.dark' },
		{ value: 'light', icon: 'sun', key: 'theme.light' },
		{ value: 'system', icon: 'monitor', key: 'theme.system' }
	];
</script>

<!-- 后台关掉「允许切换深浅色」时整个按钮不出现，内联脚本也会忽略本机已存的偏好 -->
{#if !site.theme.allowSchemeToggle}
	<!-- 不渲染 -->
{:else if variant === 'icon'}
	<button
		type="button"
		class="icon-btn"
		aria-label={i18n.t(light ? 'theme.toDark' : 'theme.toLight')}
		title={i18n.t(light ? 'theme.toDark' : 'theme.toLight')}
		onclick={() => choose(light ? 'dark' : 'light')}
	>
		<Icon name={light ? 'moon' : 'sun'} size={19} />
	</button>
{:else}
	<div class="inline-flex rounded-full bg-sunken p-1" role="radiogroup" aria-label={i18n.t('theme.label')}>
		{#each OPTIONS as option (option.value)}
			<button
				type="button"
				role="radio"
				aria-checked={preference === option.value}
				class="inline-flex h-9 items-center gap-1.5 rounded-full px-3.5 text-sm font-medium text-fg-muted transition-colors aria-checked:bg-overlay aria-checked:text-fg aria-checked:shadow-sm"
				onclick={() => choose(option.value)}
			>
				<Icon name={option.icon} size={16} />
				{i18n.t(option.key)}
			</button>
		{/each}
	</div>
{/if}
