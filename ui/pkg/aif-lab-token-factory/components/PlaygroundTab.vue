<script setup lang="ts">
import { ref, computed, onBeforeUnmount } from 'vue';
import { endpoints, sampleAnswer, gatewayBase } from '../mock';

const chat = endpoints.filter((e) => e.kind === 'chat');
const model = ref(chat[0].model);
const prompt = ref('Explain how Kubernetes schedules GPU workloads.');
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
    <label>Model</label>
    <select v-model="model" class="mb-10">
      <option v-for="e in chat" :key="e.id" :value="e.model">{{ e.model }}</option>
    </select>
    <label>Prompt</label>
    <textarea v-model="prompt" rows="4" style="width:100%" />
    <div class="mt-10 mb-10">
      <button class="btn role-primary" :disabled="running" @click="send">Send</button>
      <button v-if="running" class="btn role-secondary ml-10" @click="stop">Stop</button>
    </div>
    <div class="tf-code" style="min-height:90px">{{ output || 'Response will stream here (mock).' }}</div>
    <h3 class="mt-20">curl</h3>
    <div class="tf-code">{{ curl }}</div>
  </div>
</template>
