<script lang="ts">
	import { getI18n } from '#lib/i18n/context.ts';
	import { getSite } from '#lib/site.ts';

	type Props = { kind?: 'grid' | 'rail' | 'list'; count?: number };
	let { kind = 'grid', count = 12 }: Props = $props();

	const i18n = getI18n();
	// 标题压在封面上时卡片下方没有文字，骨架也不画，否则内容到位时会跳一下
	const overlay = $derived(getSite().layout.cardStyle === 'overlay');
</script>

<!-- 骨架块尺寸与真实卡片一致（封面比例由后台配 + 两行标题），内容到位时不跳动；色块对读屏隐藏，只播报一次「加载中」 -->
<div role="status" aria-busy="true" aria-label={i18n.t('common.loading')}>
	{#if kind === 'list'}
		<ul class="space-y-3" aria-hidden="true">
			{#each { length: count }, index (index)}
				<li class="flex gap-3">
					<span class="skeleton aspect-cover w-20 shrink-0 rounded-card"></span>
					<span class="flex flex-1 flex-col gap-2 py-1">
						<span class="skeleton h-4 w-2/3"></span>
						<span class="skeleton h-3 w-1/3"></span>
					</span>
				</li>
			{/each}
		</ul>
	{:else}
		<ul
			class={kind === 'rail'
				? 'flex gap-2.5 overflow-hidden lg:gap-4'
				: 'grid grid-cols-3 gap-x-2.5 gap-y-5 sm:grid-cols-4 md:grid-cols-5 lg:grid-cols-6 lg:gap-x-4'}
			aria-hidden="true"
		>
			{#each { length: count }, index (index)}
				<li class={kind === 'rail' ? 'w-[30%] shrink-0 sm:w-[22%] md:w-[18%] lg:w-[15.2%] xl:w-[13%]' : ''}>
					<span class="skeleton aspect-cover block rounded-card"></span>
					{#if !overlay}
						<span class="skeleton mt-2 block h-3.5 w-11/12"></span>
						<span class="skeleton mt-1.5 block h-3.5 w-1/2"></span>
					{/if}
				</li>
			{/each}
		</ul>
	{/if}
</div>
