<script setup lang="ts">
import { onMounted, ref } from "vue";
import { Bot, Plus, Trash2, Zap } from "lucide-vue-next";
import { api } from "../api";
import Modal from "../components/Modal.vue";
const providers=ref<any[]>([]),open=ref(false),editing=ref<any>(),error=ref("");
const form=ref<any>({name:"",base_url:"https://api.openai.com",model:"gpt-4.1-mini",api_key:"",timeout_seconds:120,enabled:true,headers:{}});
async function load(){providers.value=await api("/api/ai/providers")}
function create(){editing.value=undefined;form.value={name:"",base_url:"https://api.openai.com",model:"gpt-4.1-mini",api_key:"",timeout_seconds:120,enabled:true,headers:{}};open.value=true}
function edit(p:any){editing.value=p;form.value={...p,api_key:"",headers:undefined};open.value=true}
async function save(){error.value="";try{await api(editing.value?`/api/ai/providers/${editing.value.id}`:"/api/ai/providers",{method:editing.value?"PUT":"POST",body:JSON.stringify(form.value)});open.value=false;await load()}catch(e:any){error.value=e.message}}
async function test(p:any){try{await api(`/api/ai/providers/${p.id}/test`,{method:"POST"});alert("连接测试成功")}catch(e:any){alert(e.message)}}
async function remove(p:any){if(confirm(`删除模型配置“${p.name}”？`)){await api(`/api/ai/providers/${p.id}`,{method:"DELETE"});await load()}}
onMounted(load)
</script>
<template><div class="page-heading"><div><span class="eyebrow">AI 编排</span><h1>AI 模型</h1><p>管理 OpenAI Chat Completions 兼容接口。</p></div><button class="btn" @click="create"><Plus/>添加配置</button></div>
<div class="resource-grid"><article v-for="p in providers" :key="p.id" class="card resource-card"><span class="card-icon"><Bot/></span><div class="grow"><h3>{{p.name}}</h3><p>{{p.model}}</p><small>{{p.base_url}} · 密钥{{p.api_key_set?'已设置':'未设置'}}</small></div><button class="icon-btn" title="测试" @click="test(p)"><Zap/></button><button class="btn secondary small-btn" @click="edit(p)">编辑</button><button class="icon-btn danger-text" @click="remove(p)"><Trash2/></button></article></div>
<Modal v-model="open" :title="editing?'编辑 AI 模型':'添加 AI 模型'"><p v-if="error" class="error">{{error}}</p><label>名称<input v-model="form.name"/></label><label>接口地址<input v-model="form.base_url" placeholder="https://api.openai.com"/></label><label>模型<input v-model="form.model"/></label><label>API Key<input v-model="form.api_key" type="password" :placeholder="editing?'留空则保持不变':''"/></label><label>超时（秒）<input v-model.number="form.timeout_seconds" type="number" min="1"/></label><label class="checkbox-row"><input v-model="form.enabled" type="checkbox"/>启用</label><div class="panel-actions"><button class="btn secondary" @click="open=false">取消</button><button class="btn" @click="save">保存</button></div></Modal></template>
