<script lang="ts">
	// Test-only harness: composes the vendored RadioGroup Root + Items so a plain
	// .test.ts can assert the canonical radio contract (role, aria-checked, and the
	// Circle indicator rendered only for the checked item). Not matched by the
	// `src/**/*.test.ts` glob, so it never runs as a test itself.
	import * as RadioGroup from "$lib/components/ui/radio-group/index.js";

	let { value = "a" }: { value?: string } = $props();
</script>

<RadioGroup.Root {value} aria-label="Options">
	<RadioGroup.Item value="a" aria-label="Option A" />
	<RadioGroup.Item value="b" aria-label="Option B" />
</RadioGroup.Root>
