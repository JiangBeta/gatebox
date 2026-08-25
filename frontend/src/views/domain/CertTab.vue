<script setup lang="ts">
import { ref, onMounted, h } from 'vue'
import { NDataTable, NEmpty, NTag } from 'naive-ui'
import { listCertificates, type Cert } from '../../api/certificates'

const certs = ref<Cert[]>([])

function formatTime(s: string) {
  if (!s) return '-'
  return s.replace('T', ' ').slice(0, 19)
}

const columns = [
  { title: '二级域名', key: 'fqdn' },
  { title: '颁发机构', key: 'issuer' },
  { title: '到期时间', key: 'notAfter', render: (row: any) => formatTime(row.notAfter) },
  { title: '序列号', key: 'serial' },
]

async function load() {
  certs.value = await listCertificates()
}
onMounted(load)
</script>

<template>
  <n-data-table :columns="columns" :data="certs" />
  <n-empty
    v-if="certs.length === 0"
    description="暂无证书（caddy 未接入，接入后此处只读展示每二级域名的证书状态）"
    style="margin-top: 24px"
  />
</template>
