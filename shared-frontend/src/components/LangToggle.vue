<script setup lang="ts">
import { useI18n } from 'shared-frontend'
import type { Locale } from 'shared-frontend'

const { locale, setLocale } = useI18n()

function pick(next: Locale) {
  if (locale.value !== next) setLocale(next)
}
</script>

<template>
  <div class="lang-toggle" role="group" aria-label="Language">
    <button
      type="button"
      class="lang-toggle-btn"
      :class="{ active: locale === 'zh-CN' }"
      title="切换到中文"
      @click="pick('zh-CN')"
    >中</button>
    <button
      type="button"
      class="lang-toggle-btn"
      :class="{ active: locale === 'en-US' }"
      title="Switch to English"
      @click="pick('en-US')"
    >EN</button>
  </div>
</template>

<style scoped>
.lang-toggle {
  display: inline-flex;
  margin-right: 8px;
  border-radius: 4px;
  overflow: hidden;
  background: var(--c-surface0);
  border: 1px solid var(--c-surface1);
}

.lang-toggle-btn {
  padding: 2px 10px;
  font-size: 12px;
  background: transparent;
  border: none;
  color: var(--c-subtext0);
  cursor: pointer;
  font-family: inherit;
  line-height: 1.6;
}

.lang-toggle-btn:hover:not(.active) {
  background: var(--c-surface1);
  color: var(--c-text);
}

.lang-toggle-btn.active {
  background: var(--c-surface1);
  color: var(--c-text);
  font-weight: 600;
}
</style>
