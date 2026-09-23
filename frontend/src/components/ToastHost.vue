<script setup lang="ts">
import { onMounted, onUnmounted, ref } from "vue";
import { AlertCircle, CheckCircle2, Info, X } from "lucide-vue-next";
import { toastEvent, type ToastPayload } from "../toast";

type ToastItem = Required<ToastPayload> & { id: number };
const items = ref<ToastItem[]>([]);
let nextID = 1;
function remove(id: number) { items.value = items.value.filter((item) => item.id !== id); }
function receive(event: Event) {
  const detail = (event as CustomEvent<ToastPayload>).detail;
  if (!detail?.message) return;
  const item: ToastItem = { id: nextID++, message: detail.message, type: detail.type || "success", duration: detail.duration || 2800 };
  items.value.push(item);
  window.setTimeout(() => remove(item.id), item.duration);
}
onMounted(() => window.addEventListener(toastEvent, receive));
onUnmounted(() => window.removeEventListener(toastEvent, receive));
</script>

<template>
  <Teleport to="body"><div class="toast-stack" aria-live="polite" aria-atomic="false">
    <TransitionGroup name="toast">
      <div v-for="item in items" :key="item.id" class="action-toast" :class="`toast-${item.type}`" role="status">
        <CheckCircle2 v-if="item.type==='success'"/><AlertCircle v-else-if="item.type==='error'"/><Info v-else/>
        <span>{{item.message}}</span><button type="button" title="关闭" @click="remove(item.id)"><X/></button>
      </div>
    </TransitionGroup>
  </div></Teleport>
</template>

<style scoped>
.toast-stack{position:fixed;z-index:220;right:22px;bottom:22px;width:min(380px,calc(100vw - 32px));display:grid;gap:9px;pointer-events:none}.action-toast{pointer-events:auto;min-height:50px;padding:11px 12px;display:flex;align-items:center;gap:10px;border:1px solid #333;border-radius:14px;background:#171717;color:#fff;box-shadow:0 18px 45px #0004;font-size:13px}.action-toast>svg{width:18px;flex:none}.action-toast span{flex:1}.action-toast button{border:0;background:transparent;color:#aaa;padding:3px;display:grid;place-items:center;cursor:pointer}.action-toast button svg{width:15px}.toast-success>svg{color:#79c69f}.toast-error>svg{color:#ef9292}.toast-info>svg{color:#91b8df}.toast-enter-active,.toast-leave-active{transition:opacity .18s,transform .2s}.toast-enter-from,.toast-leave-to{opacity:0;transform:translateX(18px)}@media(max-width:600px){.toast-stack{right:16px;bottom:16px}}
</style>
