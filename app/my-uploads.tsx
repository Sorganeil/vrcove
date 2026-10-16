import VRChatGate from './vrchat-gate';
import {api} from './api';
import {formatDate,t as tr,useLocale} from './i18n';
import {useState,useEffect} from 'react';
import {LoaderCircle,ShieldCheck,Upload,Monitor,Smartphone,Users,RefreshCw} from 'lucide-react';
import {detailLink} from './vrchat-details';
function Photo({url,name}:any){useLocale();const [fail,setFail]=useState(false);useEffect(()=>setFail(false),[url]);return url&&!fail?<img src={'/api/image?url='+encodeURIComponent(url)} alt="" onError={()=>setFail(true)} referrerPolicy="no-referrer" loading="lazy"/>:<div className="initial-fallback" aria-hidden="true">{[...String(name||'?').trim()][0]?.toUpperCase()||'?'}</div>;}
const date=(s?:string)=>s&&Number.isFinite(new Date(s).getTime())?formatDate(s):'-';
const platforms=(item:any)=>new Set<string>((item.unityPackages||[]).filter((p:any)=>p.variant!=='impostor').map((p:any)=>p.platform));
/* Avatars and worlds uploaded by the signed-in account, including private ones. */
export default function MyUploads({coveAccount,user,onLogin,onAvatar}:{coveAccount:any;user:any;onLogin:()=>void;onAvatar:(raw:any)=>void}){useLocale();
 const [kind,setKind]=useState<'avatars'|'worlds'>('avatars'),[status,setStatus]=useState('all'),[offset,setOffset]=useState(0),[reload,setReload]=useState(0);
 const [state,setState]=useState<{key:string;rows:any[]|null;error:string}>({key:'',rows:null,error:''});
 const key=kind+':'+offset+':'+reload;
 useEffect(()=>{if(!user)return;let alive=true;api('/account/my-'+kind+'?offset='+offset).then(rows=>{if(alive)setState({key,rows:Array.isArray(rows)?rows:[],error:''});}).catch(e=>{if(alive)setState({key,rows:null,error:e.message});});return()=>{alive=false;};},[key,user?.id]);
 if(!user)return <VRChatGate coveAccount={coveAccount} section={tr("My uploads")} onConnect={onLogin}/>;
 const current=state.key===key?state:{rows:null,error:''};
 const rows=(current.rows||[]).filter(r=>status==='all'||r.releaseStatus===status);
 return <div className="entity-page uploads-page"><div className="page-title"><div><h1>{tr("My uploads")}</h1><p>{tr("Avatars and worlds published from your account, including private ones.")}</p></div><button className="standard-button" onClick={()=>setReload(r=>r+1)}><RefreshCw size={14}/>{tr("Refresh")}</button></div>
  <div className="browse-bar"><div className="chips">{(['avatars','worlds'] as const).map(k=><button key={k} className={kind===k?'chosen':''} onClick={()=>{setKind(k);setOffset(0);}}>{k==='avatars'?tr("Avatars"):tr("Worlds")}</button>)}</div><div className="chips">{['all','public','private'].map(s=><button key={s} className={status===s?'chosen':''} onClick={()=>setStatus(s)}>{tr(s[0].toUpperCase()+s.slice(1))}</button>)}</div></div>
  {current.error?<div className="error-banner" role="alert">{current.error}</div>:!current.rows?<div className="loading-state"><LoaderCircle className="spin"/>{tr("Loading your uploads…")}</div>:!rows.length?<div className="empty-state"><Upload size={28}/><h3>{tr("No uploads to show.")}</h3><p>{tr("Content you upload with the VRChat SDK appears here.")}</p></div>:
  <div className="entity-grid">{rows.map(r=>{const p=platforms(r);const inner=<><div className="entity-card-image wide"><Photo url={r.thumbnailImageUrl} name={r.name}/><span className={'release-badge '+r.releaseStatus}>{tr(r.releaseStatus==='public'?'Public':r.releaseStatus==='private'?'Private':r.releaseStatus)}</span></div><strong>{r.name}</strong><small>{p.has('standalonewindows')&&<Monitor size={11}/>}{p.has('android')&&<Smartphone size={11}/>}{kind==='worlds'&&typeof r.occupants==='number'&&<><Users size={11}/>{r.occupants} · </>}{tr("Updated")} {date(r.updated_at)}</small></>;
   return kind==='avatars'?<button className="entity-card" key={r.id} onClick={()=>onAvatar(r)}>{inner}</button>:<a className="entity-card" key={r.id} {...detailLink('world',r.id)}>{inner}</a>;})}</div>}
  {current.rows&&(offset>0||current.rows.length===100)&&<div className="pagination"><span>{tr("Items")} {offset+1}–{offset+current.rows.length}</span><div><button disabled={!offset} onClick={()=>setOffset(o=>Math.max(0,o-100))}>{tr("Previous")}</button><button disabled={current.rows.length<100} onClick={()=>setOffset(o=>o+100)}>{tr("Next")}</button></div></div>}
 </div>;
}
