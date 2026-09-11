<script>
export default {
  name: 'RepositoryHealthBanner',
  props: {
    repositories: { type: Array, default: () => [] },
    error: { type: String, default: '' },
    messages: { type: Array, default: () => [] },
  },
  computed: {
    unavailable() { return this.repositories.filter(repo => !repo.ready); },
    settingsLink() {
      return { name: 'c-cluster-suseai-settings', params: { cluster: this.$route?.params?.cluster || 'local' } };
    },
  },
};
</script>

<template>
  <div v-if="error || unavailable.length || messages.length" class="repository-health" role="status">
    <strong>{{ t(error ? 'suseai.repositoryHealth.unknown' : 'suseai.repositoryHealth.title') }}</strong>
    <p v-if="error">{{ error }}</p>
    <ul v-else>
      <li v-for="repo in unavailable" :key="repo.name">
        <router-link :to="`/c/local/apps/catalog.cattle.io.clusterrepo/${encodeURIComponent(repo.name)}`">{{ repo.name }}</router-link>
        <span> — {{ repo.message || t('suseai.repositoryHealth.pending') }}</span>
      </li>
      <li v-for="message in messages" :key="message">{{ message }}</li>
    </ul>
    <p>{{ t('suseai.repositoryHealth.guidance') }} <router-link :to="settingsLink">{{ t('suseai.repositoryHealth.settings') }}</router-link></p>
  </div>
</template>

<style lang="scss" scoped>
.repository-health {
  padding: 12px 16px;
  margin-bottom: 20px;
  border: 1px solid var(--warning);
  border-radius: var(--border-radius);
  background: var(--warning-banner-bg);
  overflow-wrap: anywhere;
  p { margin: 8px 0 0; }
  ul { margin: 8px 0; padding-left: 20px; }
}
</style>
