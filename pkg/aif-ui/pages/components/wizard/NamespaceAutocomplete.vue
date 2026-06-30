<template>
  <div class="ns-autocomplete">
    <label v-if="!labelInside" class="ns-label" :class="{ 'ns-label--required': required }">
      {{ label }}
    </label>
    <div
      class="ns-input-wrap"
      :class="{
        'ns-input-wrap--labeled': labelInside,
        'ns-input-wrap--open': isOpen && filteredOptions.length > 0,
        'ns-input-wrap--disabled': disabled,
      }"
    >
      <label v-if="labelInside" class="ns-label ns-label--inside" :class="{ 'ns-label--required': required }">
        {{ label }}
      </label>
      <div class="ns-input-row">
        <input
          ref="inputRef"
          v-model="inputValue"
          type="text"
          class="ns-input"
          :placeholder="placeholder"
          :disabled="disabled"
          :required="required"
          autocomplete="off"
          @focus="onFocus"
          @input="onInput"
          @keydown="onKeydown"
          @blur="onBlur"
        />
        <span v-if="loading" class="ns-spinner" />
      </div>
    </div>
    <ul
      ref="dropdownRef"
      v-if="isOpen && filteredOptions.length > 0"
      class="ns-dropdown"
    >
      <li
        v-for="(opt, i) in filteredOptions"
        :key="opt.value"
        class="ns-option"
        :class="{ 'ns-option--highlighted': i === highlightIndex }"
        @mousedown.prevent="select(opt.value)"
        @mouseover="highlightIndex = i"
      >
        {{ opt.label }}
      </li>
    </ul>
  </div>
</template>

<script lang="ts" setup>
import { ref, computed, watch, nextTick } from 'vue';

interface Option {
  label: string;
  value: string;
}

interface Props {
  value:        string;
  options:      Option[];
  label:        string;
  placeholder?: string;
  required?:    boolean;
  disabled?:    boolean;
  loading?:     boolean;
  labelInside?: boolean;
}

interface Emits {
  (e: 'update:value', v: string): void;
}

const props = withDefaults(defineProps<Props>(), {
  placeholder: '',
  required:    false,
  disabled:    false,
  loading:     false,
  labelInside: false,
});
const emit = defineEmits<Emits>();

const inputRef       = ref<HTMLInputElement | null>(null);
const dropdownRef    = ref<HTMLUListElement | null>(null);
const inputValue     = ref(props.value);
const isOpen         = ref(false);
const highlightIndex = ref(-1);

const filteredOptions = computed(() => {
  const q = inputValue.value.toLowerCase();
  if (!q) return props.options;
  return props.options.filter(o => o.label.toLowerCase().includes(q));
});

watch(() => props.value, (v) => { inputValue.value = v; });

watch(filteredOptions, (opts) => {
  if (highlightIndex.value >= opts.length) highlightIndex.value = -1;
});

function onFocus() {
  isOpen.value = true;
  highlightIndex.value = -1;
}

function onInput() {
  isOpen.value = true;
  highlightIndex.value = -1;
  emit('update:value', inputValue.value);
}

function onBlur() {
  isOpen.value = false;
  highlightIndex.value = -1;
  emit('update:value', inputValue.value);
}

function select(value: string) {
  inputValue.value = value;
  emit('update:value', value);
  isOpen.value = false;
  highlightIndex.value = -1;
}

async function scrollHighlightedIntoView() {
  await nextTick();
  const el = dropdownRef.value?.querySelector<HTMLElement>('.ns-option--highlighted');
  el?.scrollIntoView({ block: 'nearest' });
}

function onKeydown(e: KeyboardEvent) {
  if (!isOpen.value) {
    if (e.key === 'ArrowDown') { e.preventDefault(); isOpen.value = true; }
    return;
  }
  if (e.key === 'ArrowDown') {
    e.preventDefault();
    highlightIndex.value = Math.min(highlightIndex.value + 1, filteredOptions.value.length - 1);
    scrollHighlightedIntoView();
  } else if (e.key === 'ArrowUp') {
    e.preventDefault();
    highlightIndex.value = Math.max(highlightIndex.value - 1, -1);
    scrollHighlightedIntoView();
  } else if (e.key === 'Enter') {
    e.preventDefault();
    if (highlightIndex.value >= 0) {
      select(filteredOptions.value[highlightIndex.value].value);
    } else {
      isOpen.value = false;
    }
  } else if (e.key === 'Tab') {
    if (highlightIndex.value >= 0) {
      e.preventDefault();
      select(filteredOptions.value[highlightIndex.value].value);
    } else {
      isOpen.value = false;
    }
  } else if (e.key === 'Escape') {
    isOpen.value = false;
    highlightIndex.value = -1;
  }
}
</script>

<style scoped>
.ns-autocomplete {
  position: relative;
}

/* ── Label outside (default) ── */
.ns-label {
  display: block;
  font-size: 13px;
  font-weight: 500;
  margin-bottom: 6px;
  color: var(--body-text);
}

/* ── Label inside (labelInside prop) ── */
.ns-label--inside {
  display: block;
  position: absolute;
  top: 10px;
  margin: 0;
  font-size: 14px;
  font-weight: normal;
  color: var(--input-label);
  pointer-events: none;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  width: calc(100% - 20px);
}

.ns-label--required::after {
  content: ' *';
  color: var(--error);
}

/* ── Input wrap ── */
.ns-input-wrap {
  display: flex;
  align-items: center;
  border: 1px solid var(--border);
  border-radius: var(--border-radius);
  background: var(--input-bg);
  transition: border-color 0.15s;
}

.ns-input-wrap--labeled {
  position: relative;
  min-height: 61px;
  padding: 10px;
  box-sizing: border-box;
  align-items: stretch;
}

.ns-input-wrap:focus-within {
  border-color: var(--primary);
  outline: none;
}

.ns-input-wrap--open {
  border-bottom-left-radius: 0;
  border-bottom-right-radius: 0;
}

.ns-input-wrap--disabled {
  background-color: var(--input-disabled-bg);
  border-color: var(--input-disabled-border);
  cursor: not-allowed;
}

.ns-input-wrap--disabled .ns-label--inside {
  color: var(--input-disabled-label);
  z-index: 1;
}

.ns-input-wrap--disabled .ns-input {
  color: var(--input-disabled-text);
  background: transparent;
  cursor: not-allowed;
}

.ns-input-wrap--disabled .ns-input::placeholder {
  color: var(--input-disabled-placeholder);
}

/* ── Input row ── */
.ns-input-row {
  display: flex;
  align-items: center;
  width: 100%;
}

/* ── Input ── */
.ns-input {
  flex: 1;
  padding: 8px 12px;
  border: none;
  background: transparent;
  color: var(--body-text);
  font-size: 14px;
  outline: none;
  min-width: 0;
}

.ns-input-wrap--labeled .ns-input {
  padding: 18px 0 0 0;
  line-height: 19px;
  width: 100%;
  align-self: stretch;
}

.ns-input:disabled {
  cursor: not-allowed;
}

/* ── Spinner ── */
.ns-spinner {
  display: inline-block;
  width: 14px;
  height: 14px;
  margin-right: 10px;
  border: 2px solid var(--border);
  border-top-color: var(--primary);
  border-radius: 50%;
  animation: ns-spin 0.6s linear infinite;
  flex-shrink: 0;
}

@keyframes ns-spin {
  to { transform: rotate(360deg); }
}

/* ── Dropdown ── */
.ns-dropdown {
  position: absolute;
  top: 100%;
  left: 0;
  right: 0;
  z-index: 200;
  margin: 0;
  padding: 0;
  list-style: none;
  background: var(--body-bg, var(--input-bg));
  border: 1px solid var(--primary);
  border-top: none;
  border-radius: 0 0 var(--border-radius) var(--border-radius);
  max-height: 200px;
  overflow-y: auto;
}

.ns-option {
  padding: 8px 12px;
  font-size: 14px;
  color: var(--body-text);
  cursor: pointer;
}

.ns-option--highlighted,
.ns-option:hover {
  background: var(--accent-btn);
}
</style>
