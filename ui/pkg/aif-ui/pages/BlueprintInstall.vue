<script lang="ts" setup>
import { getCurrentInstance } from 'vue';
import BlueprintInstallWizard from './components/BlueprintInstallWizard.vue';

const vm    = getCurrentInstance()!.proxy as any;
const route = vm.$route;

const blueprintName    = (route.query.name    as string) || '';
const blueprintVersion = (route.query.version as string) || '';
</script>

<template>
  <BlueprintInstallWizard
    :blueprint-name="blueprintName"
    :blueprint-version="blueprintVersion"
  />
</template>
