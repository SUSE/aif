<script lang="ts">
import { defineComponent } from 'vue';
import AppWizard from './components/AppWizard.vue';

export default defineComponent({
  name: 'ManageApp',
  components: { AppWizard },
  props: { slug: { type: String, required: true } }
});
</script>

<template>
  <AppWizard :slug="slug" mode="manage" />
</template>