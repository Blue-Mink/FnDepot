<script setup lang="ts">
import type { TabsRootEmits, TabsRootProps } from "reka-ui";
import type { HTMLAttributes } from "vue";
import { reactiveOmit, useVModel } from "@vueuse/core";
import { TabsRoot, useForwardProps } from "reka-ui";
import { cn } from "@/lib/utils";

const props = defineProps<
  TabsRootProps & { class?: HTMLAttributes["class"] }
>();
const emits = defineEmits<TabsRootEmits>();

const delegatedProps = reactiveOmit(
  props,
  "class",
  "defaultValue",
  "modelValue",
);
const forwardedProps = useForwardProps(delegatedProps);

const modelValue = useVModel(props, "modelValue", emits, {
  passive: true,
  defaultValue: props.defaultValue,
});

const handleUpdateModelValue = (value: string | number) => {
  modelValue.value = value;
};
</script>

<template>
  <TabsRoot
    data-slot="reka-tabs"
    :model-value="modelValue"
    v-bind="forwardedProps"
    @update:model-value="handleUpdateModelValue"
    :class="cn('flex flex-col gap-4', props.class)"
  >
    <slot />
  </TabsRoot>
</template>
