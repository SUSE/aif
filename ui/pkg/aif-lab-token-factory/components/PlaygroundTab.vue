<script setup lang="ts">
import { ref, computed, onBeforeUnmount } from 'vue';
import { useStore } from 'vuex';
import { useI18n } from '@shell/composables/useI18n';
import LabeledSelect from '@shell/components/form/LabeledSelect';
import { LabeledInput } from '@components/Form/LabeledInput';
import { endpoints, sampleAnswer, gatewayBase } from '../mock';

const { t } = useI18n(useStore());

const chat = endpoints.filter((e) => e.kind === 'chat');
const modelOptions = chat.map((e) => e.model);
const model = ref(chat[0].model);
const prompt = ref(t('tokenFactory.playground.defaultPrompt'));
const output = ref('');
const running = ref(false);
let timer: ReturnType<typeof setInterval> | undefined;

function stop() {
  if (timer) clearInterval(timer);
  timer = undefined;
  running.value = false;
}
function send() {
  stop();
  output.value = '';
  running.value = true;
  let i = 0;

  timer = setInterval(() => {
    i += 3;
    output.value = sampleAnswer.slice(0, i);
    if (i >= sampleAnswer.length) stop();
  }, 30);
}
onBeforeUnmount(stop);

const curl = computed(() => `curl ${ gatewayBase }/chat/completions \\
  -H "Authorization: Bearer $TOKEN_FACTORY_KEY" \\
  -H "Content-Type: application/json" \\
  -d '${ JSON.stringify({ model: model.value, stream: true, messages: [{ role: 'user', content: prompt.value }] }) }'`);
</script>

<template>
  <div>
    <LabeledSelect
      v-model:value="model"
      class="mb-10"
      :label="t('tokenFactory.playground.model')"
      :options="modelOptions"
      :clearable="false"
    />
    <LabeledInput
      v-model:value="prompt"
      type="multiline"
      :label="t('tokenFactory.playground.prompt')"
      :min-height="100"
    />
    <div class="mt-10 mb-10">
      <button
        class="btn role-primary"
        :disabled="running"
        @click="send"
      >
        {{ t('tokenFactory.playground.send') }}
      </button>
      <button
        v-if="running"
        class="btn role-secondary ml-10"
        @click="stop"
      >
        {{ t('tokenFactory.playground.stop') }}
      </button>
    </div>
    <div class="code output">
      {{ output || t('tokenFactory.playground.placeholder') }}
    </div>
    <h3 class="mt-20">
      {{ t('tokenFactory.playground.curl') }}
    </h3>
    <div class="code">
      {{ curl }}
    </div>
  </div>
</template>

<style scoped lang="scss">
.code { background: var(--box-bg); color: var(--body-text); border: 1px solid var(--border); border-radius: var(--border-radius); padding: 12px; font-family: monospace; font-size: 12px; white-space: pre-wrap; word-break: break-all; }
.output { min-height: 90px; }
</style>
