<script lang="ts">
	import { getI18n } from '#lib/i18n/context.ts';
	import Icon from '#lib/ui/Icon.svelte';
	import Modal from '#lib/ui/Modal.svelte';
	import { REPORT_REASONS, type ReportReason } from './thread.ts';

	type Props = {
		open: boolean;
		onclose: () => void;
		onsubmit: (reason: ReportReason, detail: string) => void;
		busy: boolean;
	};
	let { open, onclose, onsubmit, busy }: Props = $props();

	const i18n = getI18n();
	let reason = $state<ReportReason | null>(null);
	let detail = $state('');

	// 每次打开都从空白开始，不带上一条的选择
	$effect(() => {
		if (open) {
			reason = null;
			detail = '';
		}
	});

	const REASON_KEYS = {
		attack: 'comment.reason.attack',
		vulgar: 'comment.reason.vulgar',
		illegal: 'comment.reason.illegal',
		minor: 'comment.reason.minor',
		ad: 'comment.reason.ad',
		spoiler: 'comment.reason.spoiler',
		other: 'comment.reason.other'
	} as const;
</script>

<Modal {open} label={i18n.t('comment.reportTitle')} {onclose} variant="sheet" size="sm">
	<div class="mb-4 flex items-start justify-between gap-3">
		<div>
			<h2 class="text-base font-bold">{i18n.t('comment.reportTitle')}</h2>
			<p class="mt-1 text-sm text-fg-muted">{i18n.t('comment.reportDesc')}</p>
		</div>
		<button type="button" class="icon-btn -mt-1 -me-2" aria-label={i18n.t('common.close')} onclick={onclose}>
			<Icon name="x" size={20} />
		</button>
	</div>

	<form
		class="flex flex-col gap-4"
		onsubmit={(event) => {
			event.preventDefault();
			if (reason && !busy) {
				onsubmit(reason, detail.trim());
			}
		}}
	>
		<fieldset>
			<legend class="mb-2 text-sm font-semibold">{i18n.t('comment.reportReason')}</legend>
			<div class="flex flex-wrap gap-2">
				{#each REPORT_REASONS as value (value)}
					<label class="chip h-10 cursor-pointer" aria-selected={reason === value}>
						<input
							type="radio"
							name="report-reason"
							class="sr-only"
							{value}
							checked={reason === value}
							onchange={() => (reason = value)}
						/>
						{i18n.t(REASON_KEYS[value])}
					</label>
				{/each}
			</div>
		</fieldset>

		<div>
			<label class="mb-1.5 block text-sm font-semibold" for="report-detail">{i18n.t('comment.reportDetail')}</label>
			<textarea bind:value={detail} id="report-detail" class="field resize-none" rows="3" maxlength="500"></textarea>
		</div>

		<button type="submit" class="btn btn-primary w-full" disabled={busy || reason === null}>
			{#if busy}<Icon name="loader-circle" size={16} class="animate-spin" />{/if}
			{i18n.t('comment.reportSubmit')}
		</button>
	</form>
</Modal>
