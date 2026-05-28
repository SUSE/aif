<template>
  <div class="wizard-step-indicator">
    <div
      v-for="(step, index) in steps"
      :key="index"
      class="wizard-step-indicator__step"
      :class="{
        'wizard-step-indicator__step--active':    index === currentStep,
        'wizard-step-indicator__step--completed': index < currentStep,
      }"
      @click="onStepClick(index)"
    >
      <div class="wizard-step-indicator__circle">
        <span v-if="index < currentStep" class="wizard-step-indicator__check">✓</span>
        <span v-else>{{ index + 1 }}</span>
      </div>
      <span class="wizard-step-indicator__label">{{ step.label }}</span>
    </div>
  </div>
</template>

<script>
// AIDEV-NOTE (P6-3): first consumer is the App-install wizard (Group 1 Task 1-1).
// See PROJECT_PLAN.md story P6-3 for acceptance criteria. Component shape may
// evolve when wired up; revisit prop names + emits there.
import { defineComponent } from 'vue';

export default defineComponent({
  name: 'WizardStepIndicator',

  props: {
    steps: {
      type:     Array,
      required: true,
    },
    currentStep: {
      type:    Number,
      default: 0,
    },
  },

  emits: ['go-to-step'],

  methods: {
    onStepClick(index) {
      if (index < this.currentStep) {
        this.$emit('go-to-step', index);
      }
    },
  },
});
</script>

<style scoped>
.wizard-step-indicator {
  display: flex;
  gap: 0;
  margin-bottom: 24px;
}

.wizard-step-indicator__step {
  display: flex;
  align-items: center;
  gap: 8px;
  flex: 1;
  opacity: 0.5;
}

.wizard-step-indicator__step--active,
.wizard-step-indicator__step--completed {
  opacity: 1;
}

.wizard-step-indicator__step--completed {
  cursor: pointer;
}

.wizard-step-indicator__circle {
  width: 28px;
  height: 28px;
  border-radius: 50%;
  border: 2px solid var(--primary);
  display: flex;
  align-items: center;
  justify-content: center;
  font-weight: 700;
  font-size: 0.8rem;
  flex-shrink: 0;
}

.wizard-step-indicator__step--active .wizard-step-indicator__circle {
  background: var(--primary);
  color: #fff;
}

.wizard-step-indicator__step--completed .wizard-step-indicator__circle {
  background: var(--success);
  border-color: var(--success);
  color: #fff;
}
</style>
