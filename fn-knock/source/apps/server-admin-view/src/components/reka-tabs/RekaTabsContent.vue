<script setup lang="ts">
import type { TabsContentProps } from "reka-ui";
import type { HTMLAttributes } from "vue";
import { reactiveOmit } from "@vueuse/core";
import { TabsContent as RekaTabsContentPrimitive } from "reka-ui";
import { cn } from "@/lib/utils";

const props = defineProps<
  TabsContentProps & { class?: HTMLAttributes["class"] }
>();

const delegatedProps = reactiveOmit(props, "class");
</script>

<template>
  <RekaTabsContentPrimitive
    data-slot="reka-tabs-content"
    v-bind="delegatedProps"
    :class="
      cn(
        'min-h-0 flex-1 outline-none motion-safe:data-[state=active]:animate-in motion-safe:data-[state=active]:fade-in-0 motion-safe:data-[state=active]:slide-in-from-bottom-2 motion-safe:data-[state=active]:duration-200',
        props.class,
      )
    "
  >
    <slot />
  </RekaTabsContentPrimitive>
</template>
