<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { api } from '../api'
type Project={id:number,name:string,created_at:string}
const projects=ref<Project[]>([]),name=ref(''),error=ref(''),newToken=ref('')
async function load(){projects.value=await api('/api/projects')}
async function create(){error.value='';try{const result=await api<Project&{token:string}>('/api/projects',{method:'POST',body:JSON.stringify({name:name.value})});newToken.value=result.token;name.value='';await load()}catch(e){error.value=(e as Error).message}}
function copyToken(){window.navigator.clipboard.writeText(newToken.value)}
onMounted(load)
</script>
<template>
  <div class="page-title"><div><h1>项目</h1><p>管理项目及其 CI 发布产物。</p></div></div>
  <section class="card"><form class="inline" @submit.prevent="create"><input v-model="name" placeholder="新项目名称" required><button>创建项目</button></form><p v-if="error" class="error">{{error}}</p><div v-if="newToken" class="token-once"><strong>项目 Token 仅显示一次，请立即保存</strong><code>{{newToken}}</code><button class="quiet" @click="copyToken">复制</button></div></section>
  <section class="grid"><router-link v-for="project in projects" :key="project.id" :to="`/projects/${project.id}`" class="card project"><h2>{{project.name}}</h2><small>创建于 {{new Date(project.created_at).toLocaleString()}}</small></router-link><p v-if="!projects.length" class="muted">还没有项目。</p></section>
</template>
