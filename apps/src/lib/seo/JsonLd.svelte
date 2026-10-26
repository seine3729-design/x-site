<script lang="ts">
	type Props = { data: Record<string, unknown> };
	let { data }: Props = $props();

	// 结构化数据（schema.org）。JSON 里的 < 转义成 \u003c，剧名等内容即使含闭合标签也不会提前结束脚本块
	const json = $derived(JSON.stringify(data).replace(/</g, '\\u003c'));
	// 标签名拆开写：组件源码里不能直接出现完整的脚本标签字面量
	const open = '<' + 'script type="application/ld+json">';
	const close = '<' + '/script>';
</script>

<svelte:head>
	<!-- eslint-disable-next-line svelte/no-at-html-tags -- 只输出上面转义过的 JSON，不含任何用户可控的 HTML -->
	{@html open + json + close}
</svelte:head>
