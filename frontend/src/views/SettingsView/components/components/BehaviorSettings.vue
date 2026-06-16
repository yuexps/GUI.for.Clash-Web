<script lang="ts" setup>
import { useAppSettingsStore } from '@/stores'

const appSettings = useAppSettingsStore()
</script>

<template>
  <div class="px-8 py-12 text-18 font-bold">{{ $t('settings.behavior') }}</div>

  <Card>
    <div class="px-8 py-12 flex items-center justify-between">
      <div class="text-16 font-bold">{{ $t('settings.autoStartKernel') }}</div>
      <Switch v-model="appSettings.app.autoStartKernel" />
    </div>
  </Card>
</template>
