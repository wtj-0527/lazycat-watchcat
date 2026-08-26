<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { api } from '@/api'
import { usePagination, usePolling } from '@/composables'
import { ago, dateTime } from '@/utils'
import AppPagination from '@/components/AppPagination.vue'
import PageState from '@/components/PageState.vue'
import { appConfirm, appPrompt } from '@/dialog'

interface Endpoint { id:string; name:string; model:string; remarkName:string; online:boolean; bindingTime?:string; loginTime?:string }
interface Session { endDeviceId:string; loginAt:string; logoutAt?:string; durationSeconds:number }
interface UserItem { deviceId:string;deviceName:string;local:boolean;userId:string;nickname:string;role:string;appInstallPermission:boolean;appAccessNoLimit:boolean;allowedAppIds:string[]|null;online:boolean;activeDevices:number;totalDevices:number;applicationCount:number;instanceCount:number;firstObservedAt:string;updatedAt:string;lastLoginAt?:string;lastLogoutAt?:string;onlineSeconds24h:number;onlineSeconds7d:number;onlineSeconds30d:number;loginCount:number;devices:Endpoint[]|null;sessions:Session[]|null }
interface Payload {items:UserItem[];count:number;recordingSince?:string;updatedAt:string}
interface ApplicationItem {id:string;title:string;devices:{deviceId:string}[]|null}
interface ApplicationsPayload {items:ApplicationItem[]}
const emit=defineEmits<{toast:[message:string]}>()
const query=ref('');const device=ref('all');const status=ref('all');const selectedKey=ref('')
const showCreate=ref(false);const newUser=ref({userId:'',password:'',role:'normal'});const busy=ref(false)
const accessMode=ref<'all'|'selected'>('all');const accessSearch=ref('');const allowedAppIds=ref<string[]>([]);const accessBusy=ref(false)
const {data,loading,error,refresh}=usePolling(()=>api<Payload>('/api/v1/users'))
const {data:applications}=usePolling(()=>api<ApplicationsPayload>('/api/v1/applications'))
const devices=computed(()=>[...new Map((data.value?.items||[]).map(x=>[x.deviceId,x.deviceName||x.deviceId])).entries()])
const filtered=computed(()=>(data.value?.items||[]).filter(x=>(device.value==='all'||x.deviceId===device.value)&&(status.value==='all'||(status.value==='online')===x.online)&&`${x.nickname} ${x.userId} ${x.deviceName}`.toLowerCase().includes(query.value.toLowerCase())))
const selected=computed(()=>filtered.value.find(x=>`${x.deviceId}\0${x.userId}`===selectedKey.value)||filtered.value[0])
const selectedEndpoints=computed(()=>selected.value?.devices||[])
const selectedSessions=computed(()=>selected.value?.sessions||[])
const appOptions=computed(()=>(applications.value?.items||[]).filter(app=>(app.devices||[]).some(x=>x.deviceId===selected.value?.deviceId)).filter(app=>`${app.title} ${app.id}`.toLowerCase().includes(accessSearch.value.trim().toLowerCase())))
const userPagination=usePagination(filtered,20)
const appAccessPagination=usePagination(appOptions,20)
const endpointPagination=usePagination(selectedEndpoints,10)
const sessionPagination=usePagination(selectedSessions,10)
watch([query,device,status],()=>{userPagination.resetPage()})
watch(userPagination.page,()=>{const item=userPagination.pagedItems.value[0];if(item)selectedKey.value=`${item.deviceId}\0${item.userId}`})
watch(filtered,(items)=>{if(items.length&&!items.some(x=>`${x.deviceId}\0${x.userId}`===selectedKey.value))selectedKey.value=`${items[0].deviceId}\0${items[0].userId}`},{immediate:true})
watch(selected, item=>{userPagination.resetPage();appAccessPagination.resetPage();endpointPagination.resetPage();sessionPagination.resetPage();if(!item)return;accessMode.value=item.appInstallPermission||item.appAccessNoLimit?'all':'selected';allowedAppIds.value=[...(item.allowedAppIds||[])];accessSearch.value=''},{immediate:true})
watch(accessSearch,appAccessPagination.resetPage)
const onlineCount=computed(()=>(data.value?.items||[]).filter(x=>x.online).length)
const duration=(seconds?:number)=>{const value=Number.isFinite(seconds)?Number(seconds):0;const h=Math.floor(value/3600),m=Math.floor((value%3600)/60);return h?`${h} 小时 ${m} 分钟`:`${m} 分钟`}
const presence=(item:UserItem)=>item.online?'在线':item.totalDevices>0?'离线':'未发现终端'
const presenceClass=(item:UserItem)=>item.online?'healthy':'unknown'
function toggleApp(id:string){allowedAppIds.value=allowedAppIds.value.includes(id)?allowedAppIds.value.filter(x=>x!==id):[...allowedAppIds.value,id]}
async function createUser(){busy.value=true;try{await api('/api/v1/users',{method:'POST',body:JSON.stringify(newUser.value)});emit('toast','用户已创建');showCreate.value=false;newUser.value={userId:'',password:'',role:'normal'};await refresh()}catch(e){emit('toast',e instanceof Error?e.message:String(e))}finally{busy.value=false}}
async function changeRole(item:UserItem){if(!await appConfirm({title:'调整用户角色',message:`确认将 ${item.nickname} 调整为${item.role==='admin'?'普通用户':'管理员'}？`,confirmText:'确认调整'}))return;try{await api(`/api/v1/users/${encodeURIComponent(item.userId)}/role`,{method:'PUT',body:JSON.stringify({role:item.role==='admin'?'normal':'admin'})});emit('toast','角色已更新');await refresh()}catch(e){emit('toast',e instanceof Error?e.message:String(e))}}
async function resetPassword(item:UserItem){const password=await appPrompt({title:'重置用户密码',message:`为 ${item.nickname} 设置新密码，至少 8 位。`,inputType:'password',inputPlaceholder:'输入新密码',confirmText:'确认重置'});if(!password)return;try{await api(`/api/v1/users/${encodeURIComponent(item.userId)}/password`,{method:'PUT',body:JSON.stringify({password})});emit('toast','密码已重置')}catch(e){emit('toast',e instanceof Error?e.message:String(e))}}
async function removeUser(item:UserItem){if(!await appConfirm({title:'删除用户',message:`确认删除用户 ${item.nickname}？本次不会清理用户数据。`,confirmText:'删除用户',danger:true}))return;try{await api(`/api/v1/users/${encodeURIComponent(item.userId)}`,{method:'DELETE'});emit('toast','用户已删除');await refresh()}catch(e){emit('toast',e instanceof Error?e.message:String(e))}}
async function saveAppAccess(item:UserItem){accessBusy.value=true;try{await api(`/api/v1/users/${encodeURIComponent(item.userId)}/app-access`,{method:'PUT',body:JSON.stringify({noLimit:item.appInstallPermission||accessMode.value==='all',allowedAppIds:item.appInstallPermission?[]:allowedAppIds.value})});emit('toast','应用可见范围已更新');await refresh()}catch(e){emit('toast',e instanceof Error?e.message:String(e))}finally{accessBusy.value=false}}
</script>

<template>
<PageState :loading="loading" :error="error" :empty="data?.items.length===0" empty-title="尚无用户数据" empty-text="用户状态将在首次真实采集后显示。" @retry="refresh">
  <div class="page-intro"><div><h2>用户</h2><p v-if="data?.recordingSince">登录历史自 {{ dateTime(data.recordingSince) }} 开始记录</p></div><button class="primary-button" @click="showCreate=!showCreate">创建本机用户</button></div>
  <section class="stats user-stats"><div class="stat"><span>用户记录</span><strong>{{ data?.count||0 }}</strong></div><div class="stat"><span>当前在线</span><strong>{{ onlineCount }}</strong></div><div class="stat"><span>登录终端</span><strong>{{ data?.items.reduce((n,x)=>n+x.activeDevices,0)||0 }}</strong></div><div class="stat"><span>运行实例</span><strong>{{ data?.items.reduce((n,x)=>n+x.instanceCount,0)||0 }}</strong></div></section>
  <section v-if="showCreate" class="card user-create-card"><div class="section-title"><div><h2>创建本机用户</h2></div></div><div class="user-form"><label><span>用户 ID</span><input v-model="newUser.userId"></label><label><span>初始密码</span><input v-model="newUser.password" type="password"></label><label><span>角色</span><select v-model="newUser.role"><option value="normal">普通用户</option><option value="admin">管理员</option></select></label><button class="primary-button" :disabled="busy" @click="createUser">{{busy?'创建中…':'确认创建'}}</button></div></section>
  <section class="card user-filter-card"><div class="filter-bar"><label class="search-field"><input v-model="query" placeholder="搜索昵称、UID 或设备"></label><select v-model="device"><option value="all">全部设备</option><option v-for="[id,name] in devices" :key="id" :value="id">{{name}}</option></select><select v-model="status"><option value="all">全部状态</option><option value="online">在线</option><option value="offline">离线</option></select></div></section>
  <div class="user-layout">
    <section class="card user-list"><button v-for="item in userPagination.pagedItems.value" :key="`${item.deviceId}-${item.userId}`" :class="{active:selected?.deviceId===item.deviceId&&selected?.userId===item.userId}" @click="selectedKey=`${item.deviceId}\0${item.userId}`"><span class="user-list-avatar">{{(item.nickname||item.userId).slice(0,1).toUpperCase()}}</span><span><b>{{item.nickname||item.userId}}</b><small>{{item.deviceName}} · {{item.userId}}</small></span><span class="pill" :class="presenceClass(item)">{{presence(item)}}</span></button><AppPagination v-model:page="userPagination.page.value" v-model:page-size="userPagination.pageSize.value" :total="userPagination.total.value" :page-count="userPagination.pageCount.value" :range-start="userPagination.rangeStart.value" :range-end="userPagination.rangeEnd.value" label="用户列表分页" /></section>
    <section v-if="selected" class="card user-detail">
      <div class="user-detail-head"><div class="user-list-avatar large">{{(selected.nickname||selected.userId).slice(0,1).toUpperCase()}}</div><div><h2>{{selected.nickname}}</h2><p>{{selected.userId}} · {{selected.deviceName}}</p></div><span class="pill" :class="presenceClass(selected)">{{presence(selected)}}</span></div>
      <div class="user-metric-grid"><div><span>最近登录</span><b>{{dateTime(selected.lastLoginAt)}}</b></div><div><span>当前终端</span><b>{{selected.activeDevices}} / {{selected.totalDevices}}</b></div><div><span>24 小时在线</span><b>{{duration(selected.onlineSeconds24h)}}</b></div><div><span>7 天在线</span><b>{{duration(selected.onlineSeconds7d)}}</b></div><div><span>30 天在线</span><b>{{duration(selected.onlineSeconds30d)}}</b></div><div><span>登录次数</span><b>{{selected.loginCount}}</b></div><div><span>应用 / 实例</span><b>{{selected.applicationCount}} / {{selected.instanceCount}}</b></div><div><span>角色</span><b>{{selected.role==='admin'?'管理员':'普通用户'}}</b></div></div>
      <div v-if="selected.local" class="user-actions"><button class="secondary-button" @click="changeRole(selected)">调整角色</button><button class="secondary-button" @click="resetPassword(selected)">重置密码</button><button class="danger-button" @click="removeUser(selected)">删除用户</button></div><p v-else class="muted">远端设备当前仅查看；管理操作需进入该设备上的 WatchCat。</p>
      <div class="section-title compact"><div><h3>应用可见范围</h3><p>LazyCat 当前按应用 ID 授权；同一应用的多个部署实例不能分别设置。</p></div></div>
      <div class="app-access-panel">
        <div class="access-mode" :class="{single:selected.appInstallPermission}">
          <button :class="{active:accessMode==='all'}" :disabled="!selected.local" @click="accessMode='all'"><b>全部应用</b><small>该用户可以访问本机所有应用</small></button>
          <button v-if="!selected.appInstallPermission" :class="{active:accessMode==='selected'}" :disabled="!selected.local" @click="accessMode='selected'"><b>指定应用</b><small>仅允许访问下面选中的应用</small></button>
        </div>
        <template v-if="accessMode==='selected'">
          <label class="app-access-search"><input v-model="accessSearch" placeholder="搜索应用名称或 App ID"></label>
          <div class="app-access-list">
            <button v-for="app in appAccessPagination.pagedItems.value" :key="app.id" :class="{selected:allowedAppIds.includes(app.id)}" :disabled="!selected.local" @click="toggleApp(app.id)">
              <span><b>{{app.title||app.id}}</b><small>{{app.id}}</small></span><i>{{allowedAppIds.includes(app.id)?'已允许':'未允许'}}</i>
            </button>
            <p v-if="!appOptions.length" class="inline-empty">该设备尚未采集到可配置的应用。</p>
          </div>
          <AppPagination v-model:page="appAccessPagination.page.value" v-model:page-size="appAccessPagination.pageSize.value" :total="appAccessPagination.total.value" :page-count="appAccessPagination.pageCount.value" :range-start="appAccessPagination.rangeStart.value" :range-end="appAccessPagination.rangeEnd.value" label="应用权限列表分页" />
        </template>
        <div v-if="selected.local" class="app-access-footer"><span v-if="accessMode==='selected'">已选择 {{allowedAppIds.length}} 个应用</span><span v-else>不限制应用访问</span><button class="primary-button" :disabled="accessBusy" @click="saveAppAccess(selected)">{{accessBusy?'保存中…':'保存可见范围'}}</button></div>
        <p v-else class="muted">远端用户的权限为只读；请在 {{selected.deviceName}} 上修改。</p>
      </div>
      <div class="section-title compact"><div><h3>登录终端</h3></div></div><div class="user-endpoints"><div v-for="endpoint in endpointPagination.pagedItems.value" :key="endpoint.id"><i :class="{online:endpoint.online}"/><span><b>{{endpoint.remarkName||endpoint.name||endpoint.model||'未知终端'}}</b><small>{{endpoint.model||endpoint.id}} · {{endpoint.online?'登录于 '+dateTime(endpoint.loginTime):'当前离线'}}</small></span></div></div><AppPagination v-model:page="endpointPagination.page.value" v-model:page-size="endpointPagination.pageSize.value" :total="endpointPagination.total.value" :page-count="endpointPagination.pageCount.value" :range-start="endpointPagination.rangeStart.value" :range-end="endpointPagination.rangeEnd.value" label="登录终端分页" />
      <div class="section-title compact"><div><h3>登录历史</h3></div></div><div class="session-timeline"><div v-for="session in sessionPagination.pagedItems.value" :key="`${session.endDeviceId}-${session.loginAt}`"><i :class="{open:!session.logoutAt}"/><span><b>{{dateTime(session.loginAt)}}</b><small>{{session.logoutAt?'退出 '+dateTime(session.logoutAt):'当前在线'}} · {{duration(session.durationSeconds)}}</small></span></div><p v-if="!selectedSessions.length" class="inline-empty">从开始记录以来尚未观察到登录会话。</p></div><AppPagination v-model:page="sessionPagination.page.value" v-model:page-size="sessionPagination.pageSize.value" :total="sessionPagination.total.value" :page-count="sessionPagination.pageCount.value" :range-start="sessionPagination.rangeStart.value" :range-end="sessionPagination.rangeEnd.value" label="登录历史分页" />
    </section>
  </div>
</PageState>
</template>
