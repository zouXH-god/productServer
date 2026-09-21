<script setup lang="ts">
import { session } from './api'
import { useRouter } from 'vue-router'
const router = useRouter()
function logout() { session.token = ''; router.push('/login') }
</script>
<template><header><router-link to="/" class="brand">CI 产物管理</router-link><nav v-if="session.token"><router-link to="/settings/ssh">SSH 连接</router-link><button class="quiet" @click="logout">退出</button></nav></header><main><router-view /></main></template>
