<template>
  <div class="ai-factory-page">
    <h1>{{ t('aif.pages.pendingReviews.title') }}</h1>
    <p>{{ t('aif.pages.pendingReviews.comingSoon') }}</p>
  </div>
</template>

<script>
export default {
  name: 'PendingReviewsPage'
};
</script>

<style scoped>
.ai-factory-page {
  padding: 20px;
}

.ai-factory-page h1 {
  margin-bottom: 10px;
}
</style>
