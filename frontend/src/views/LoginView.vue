<script setup lang="ts">
import { ref } from 'vue'; import { useRouter } from 'vue-router'; import { api, session } from '../api'
const username=ref('admin'), password=ref(''), error=ref(''), loading=ref(false), router=useRouter()
async function submit(){ loading.value=true;error.value='';try{const r=await api<{token:string}>('/api/auth/login',{method:'POST',body:JSON.stringify({username:username.value,password:password.value})});session.token=r.token;router.push('/')}catch(e){error.value=(e as Error).message}finally{loading.value=false} }
</script>
<template><section class="card narrow"><h1>登录</h1><form @submit.prevent="submit"><label>用户名<input v-model="username" required></label><label>密码<input v-model="password" type="password" required autofocus></label><p v-if="error" class="error">{{error}}</p><button :disabled="loading">{{loading?'登录中…':'登录'}}</button></form></section></template>
