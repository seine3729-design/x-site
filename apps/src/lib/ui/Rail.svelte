<script lang="ts" generics="T">
	import type { Snippet } from 'svelte';
	import { getI18n } from '#lib/i18n/context.ts';
	import Icon from './Icon.svelte';

	type Props = {
		items: T[];
		key: (item: T) => string | number;
		item: Snippet<[T, number]>;
		/** 每项宽度（Tailwind 类），默认海报宽度：手机露出 3 张多一点 */
		itemClass?: string;
		label: string;
	};

	let {
		items,
		key,
		item,
		itemClass = 'w-[30%] sm:w-[22%] md:w-[18%] lg:w-[15.2%] xl:w-[13%]',
		label
	}: Props = $props();

	const i18n = getI18n();
	let scroller = $state<HTMLUListElement>();
	let atStart = $state(true);
	let atEnd = $state(false);

	function update() {
		const el = scroller;
		if (!el) {
			return;
		}
		// RTL 下 scrollLeft 从 0 往负数走，取绝对值统一判断
		const offset = Math.abs(el.scrollLeft);
		atStart = offset < 4;
		atEnd = offset + el.clientWidth >= el.scrollWidth - 4;
	}

	function page(direction: 1 | -1) {
		const el = scroller;
		if (!el) {
			return;
		}
		const rtl = getComputedStyle(el).direction === 'rtl';
		el.scrollBy({ left: direction * (rtl ? -1 : 1) * el.clientWidth * 0.85, behavior: 'smooth' });
	}

	$effect(() => {
		update();
	});
</script>

<div class="group/rail relative">
	<!-- 滚动容器必须是定位元素：否则里面的绝对定位子元素（如 sr-only 文本）以外层为包含块，
	     不被横向滚动裁剪，整页会多出横向滚动条 -->
	<ul
		bind:this={scroller}
		onscroll={update}
		class="no-scrollbar relative -mx-4 flex snap-x snap-mandatory scroll-px-4 gap-2.5 overflow-x-auto px-4 md:-mx-6 md:scroll-px-6 md:px-6 lg:mx-0 lg:scroll-px-0 lg:gap-4 lg:px-0"
		aria-label={label}
	>
		{#each items as entry, index (key(entry))}
			<li class="shrink-0 snap-start {itemClass}">
				{@render item(entry, index)}
			</li>
		{/each}
	</ul>
	<!-- 电脑端左右翻页按钮（触屏直接滑动） -->
	<button
		type="button"
		class="absolute start-0 top-[38%] z-10 hidden size-10 -translate-x-1/2 -translate-y-1/2 place-items-center rounded-full border border-line bg-overlay/95 text-fg shadow-pop transition-opacity duration-200 hover:bg-raised rtl:translate-x-1/2 lg:grid {atStart
			? 'pointer-events-none opacity-0'
			: 'opacity-0 group-hover/rail:opacity-100 focus-visible:opacity-100'}"
		aria-label={i18n.t('common.prev')}
		tabindex={atStart ? -1 : 0}
		onclick={() => page(-1)}
	>
		<Icon name="chevron-left" size={20} class="rtl:rotate-180" />
	</button>
	<button
		type="button"
		class="absolute end-0 top-[38%] z-10 hidden size-10 translate-x-1/2 -translate-y-1/2 place-items-center rounded-full border border-line bg-overlay/95 text-fg shadow-pop transition-opacity duration-200 hover:bg-raised rtl:-translate-x-1/2 lg:grid {atEnd
			? 'pointer-events-none opacity-0'
			: 'opacity-0 group-hover/rail:opacity-100 focus-visible:opacity-100'}"
		aria-label={i18n.t('common.next')}
		tabindex={atEnd ? -1 : 0}
		onclick={() => page(1)}
	>
		<Icon name="chevron-right" size={20} class="rtl:rotate-180" />
	</button>
</div>
