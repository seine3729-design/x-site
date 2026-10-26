<script lang="ts">
	import { getI18n } from '#lib/i18n/context.ts';
	import Icon from '#lib/ui/Icon.svelte';

	type Props = {
		id: string;
		name: string;
		label: string;
		autocomplete: 'current-password' | 'new-password';
		describedby?: string;
		invalid?: boolean;
		value?: string;
	};
	let { id, name, label, autocomplete, describedby, invalid = false, value = $bindable('') }: Props = $props();

	const i18n = getI18n();
	let visible = $state(false);
</script>

<div>
	<label for={id} class="mb-1.5 block text-sm font-medium text-fg">{label}</label>
	<div class="relative">
		<input
			{id}
			{name}
			type={visible ? 'text' : 'password'}
			bind:value
			required
			minlength={autocomplete === 'new-password' ? 8 : undefined}
			maxlength="128"
			{autocomplete}
			aria-describedby={describedby}
			aria-invalid={invalid ? 'true' : undefined}
			class="field pe-12"
		/>
		<button
			type="button"
			class="absolute end-1.5 top-1/2 grid size-9 -translate-y-1/2 place-items-center rounded-full text-fg-subtle hover:text-fg"
			aria-label={i18n.t(visible ? 'auth.hidePassword' : 'auth.showPassword')}
			aria-pressed={visible}
			onclick={() => (visible = !visible)}
		>
			<Icon name={visible ? 'eye-off' : 'eye'} size={18} />
		</button>
	</div>
</div>
