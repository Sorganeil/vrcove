import {useSyncExternalStore,useState,useEffect} from 'react';

export function useSessionTime(){
 const [now,setNow]=useState(()=>Date.now());
 useEffect(()=>{const timer=window.setInterval(()=>setNow(Date.now()),30_000);return()=>window.clearInterval(timer);},[]);
 return now;
}

export type VRChatSessionIssue='vrchat_session_expired'|'vrchat_session_invalid'|'vrchat_session_required';
type Connection={expiresAt:number|null;issue:VRChatSessionIssue|null;userId:string|null};
let connection:Connection={expiresAt:null,issue:null,userId:null};
let revision=0;
let accountEpoch=0;
const listeners=new Set<()=>void>();
function update(next:Connection){
 if(next.expiresAt===connection.expiresAt&&next.issue===connection.issue&&next.userId===connection.userId)return;
 connection=next;listeners.forEach(listener=>listener());
}
const subscribe=(listener:()=>void)=>{listeners.add(listener);return()=>{listeners.delete(listener);};};
export function useVRChatSession(){return useSyncExternalStore(subscribe,()=>connection);}
export function resetVRChatSession(){revision++;accountEpoch++;update({expiresAt:null,issue:null,userId:null});}
export function expireVRChatSession(){revision++;update({...connection,expiresAt:null,issue:'vrchat_session_expired'});}
export class ApiError extends Error{constructor(message:string,public status:number,public code?:string){super(message);}}

// All VRChat panels share connection state, including failures during actions.
export async function api(path:string,body?:unknown):Promise<any>{
 const activating=path==='/account/login'||path==='/account/2fa'||path==='/account/switch';
 if(path==='/account/switch')revision++;
 const started=revision,epoch=accountEpoch;
 const response=await fetch('/api'+path,{method:body===undefined?'GET':'POST',headers:{...(body===undefined?{}:{'Content-Type':'application/json'}),...(path.startsWith('/account/')&&path!=='/account/me'&&!activating&&connection.userId?{'X-VRChat-Active-User':connection.userId}:{})},body:body===undefined?undefined:JSON.stringify(body)});
 const data=await response.json();
 const responseUser=response.headers.get('X-VRChat-Session-User');
 const belongs=!responseUser||!connection.userId||responseUser===connection.userId;
 if(!response.ok){
  if(data.code==='vrchat_account_changed')window.dispatchEvent(new Event('vrcove:account-changed'));
  if(started===revision&&belongs&&['vrchat_session_expired','vrchat_session_invalid','vrchat_session_required'].includes(data.code)){
   revision++;update({...connection,expiresAt:null,issue:data.code});
  }
  throw new ApiError(data.error||'Request failed',response.status,data.code);
 }
 if(activating&&data.user&&epoch===accountEpoch){
  revision++;
  const expiry=Number(response.headers.get('X-VRChat-Session-Expires'));
  update({expiresAt:expiry>0?expiry:null,issue:null,userId:data.user.id});
  return data;
 }
 if((started!==revision||(!belongs&&path!=='/account/me'))&&path.startsWith('/account/'))throw new ApiError('Your VRChat connection changed. Please try again.',409);
 if(started===revision){
  if(path==='/account/me'&&data.id){
   if(connection.issue||(connection.userId&&connection.userId!==data.id))revision++;
   update({...connection,issue:null,userId:data.id});
  }
  const expiresAt=Number(response.headers.get('X-VRChat-Session-Expires'));
  if(Number.isFinite(expiresAt)&&expiresAt>0)update({...connection,expiresAt});
 }
 return data;
}

// A freshly verified active connection takes precedence over older link
// metadata. Known expiry or invalidation still wins over cached user data.
export function vrchatAccountState(link:{vrchatId:string;sessionExpires?:number},userId:string|undefined,current:Connection,now:number):'active'|'available'|'reconnect'{
 const selected=current.userId===link.vrchatId;
 if(selected&&(current.issue!==null||(current.expiresAt!==null&&current.expiresAt<=now)))return 'reconnect';
 if(userId===link.vrchatId&&!current.issue)return 'active';
 return link.sessionExpires&&link.sessionExpires>now?'available':'reconnect';
}
