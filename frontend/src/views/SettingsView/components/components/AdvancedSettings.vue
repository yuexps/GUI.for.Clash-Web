<script lang="ts" setup>
import { useAppSettingsStore } from '@/stores'

const appSettings = useAppSettingsStore()
</script>

<template>
  <div class="px-8 py-12 text-18 font-bold">{{ $t('settings.advanced') }}</div>

  <Card>
    <div class="px-8 py-12 flex items-center justify-between">
      <div class="text-16 font-bold">{{ $t('settings.realMemoryUsage') }}</div>
      <Switch v-model="appSettings.app.kernel.realMemoryUsage" />
    </div>
    <div class="px-8 py-12 flex items-center justify-between">
      <div class="text-16 font-bold">
        {{ $t('settings.autoRestartKernel.name') }}
        <span class="font-normal text-12">({{ $t('settings.autoRestartKernel.tips') }})</span>
      </div>
      <Switch v-model="appSettings.app.autoRestartKernel" />
    </div>
  </Card>
</template>
