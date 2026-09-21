<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { api, authDownload } from '../api'
type Token={id:number;prefix:string;created_at:string;last_used_at?:string}
type File={id:number;name:string;size:number;sha256:string;mime_type:string}
type Release={id:number;version:string;branch:string;commit_sha:string;created_at:string;files?:File[]}
type Project={id:number;name:string}
const route=useRoute(),router=useRouter(),id=Number(route.params.id)
const project=ref<Project>(),token=ref<Token>(),releases=ref<Release[]>([]),newToken=ref(''),selected=ref<Release>(),error=ref('')
async function load(){try{[project.value,token.value,releases.value]=await Promise.all([api<Project>(`/api/projects/${id}`),api<Token>(`/api/projects/${id}/token`),api<Release[]>(`/api/projects/${id}/releases`)])}catch(e){error.value=(e as Error).message}}
async function rotate(){if(!confirm('轮换后旧 Token 将立即失效，确定继续？'))return;const result=await api<Token&{token:string}>(`/api/projects/${id}/token/rotate`,{method:'POST'});newToken.value=result.token;await load()}
async function openRelease(release:Release){selected.value=await api(`/api/projects/${id}/releases/${release.id}`)}
async function removeProject(){if(confirm('删除项目及其所有产物？此操作不可撤销。')){await api(`/api/projects/${id}`,{method:'DELETE'});router.push('/')}}
function size(n:number){if(n<1024)return `${n} B`;if(n<1048576)return `${(n/1024).toFixed(1)} KB`;return `${(n/1048576).toFixed(1)} MB`}
function copy(value:string){window.navigator.clipboard.writeText(value)}
function downloadTemplate(file:File){return `${location.origin}/api/download?token=PROJECT_TOKEN&version=${encodeURIComponent(selected.value!.version)}&file=${encodeURIComponent(file.name)}`}
onMounted(load)
</script>
<template><div v-if="project">
  <div class="page-title"><div><router-link to="/">← 项目</router-link><h1>{{project.name}}</h1></div><button class="danger" @click="removeProject">删除项目</button></div><p v-if="error" class="error">{{error}}</p>
  <section class="card"><h2>项目 Token</h2><p>该 Token 可上传和下载项目产物。当前前缀：<code>{{token?.prefix}}…</code>，最后使用：{{token?.last_used_at?new Date(token.last_used_at).toLocaleString():'从未'}}</p><button @click="rotate">轮换 Token</button><div v-if="newToken" class="token-once"><strong>仅显示一次，请立即保存</strong><code>{{newToken}}</code><button class="quiet" @click="copy(newToken)">复制</button></div></section>
  <section class="card"><h2>GitHub Action</h2><pre><code>- uses: shiran/product-server-action@v1
  with:
    url: $&#123;&#123; secrets.ARTIFACT_SERVER_URL &#125;&#125;
    token: $&#123;&#123; secrets.ARTIFACT_SERVER_TOKEN &#125;&#125;
    path: dist/**
    name: artifact</code></pre></section>
  <section class="card"><h2>发布版本</h2><table><thead><tr><th>版本</th><th>分支</th><th>提交</th><th>时间</th></tr></thead><tbody><tr v-for="release in releases" :key="release.id" class="clickable" @click="openRelease(release)"><td><strong>{{release.version}}</strong></td><td>{{release.branch||'—'}}</td><td><code>{{release.commit_sha?.slice(0,10)||'—'}}</code></td><td>{{new Date(release.created_at).toLocaleString()}}</td></tr></tbody></table><p v-if="!releases.length" class="muted">尚未上传发布产物。</p></section>
  <section v-if="selected" class="card"><h2>{{selected.version}} 的文件</h2><table><tbody><tr v-for="file in selected.files" :key="file.id"><td>{{file.name}}</td><td>{{size(file.size)}}</td><td><code :title="file.sha256">{{file.sha256.slice(0,12)}}…</code></td><td><button class="link" @click="authDownload(`/api/projects/${id}/releases/${selected!.id}/files/${file.id}/download`,file.name)">下载</button><button class="link" @click="copy(downloadTemplate(file))">复制直链模板</button></td></tr></tbody></table></section>
</div></template>
