import {api} from './api';
import {t as tr,useLocale} from './i18n';
import {useState,useEffect,useRef} from 'react';
import {Search,X,LoaderCircle,UserRound,ArrowUpRight,ShieldCheck} from 'lucide-react';
import {openDetail,type Kind} from './vrchat-details';
function Thumb({src}:{src?:string}){useLocale();const [failed,setFailed]=useState(false);useEffect(()=>setFailed(false),[src]);return src&&!failed?<img src={'/api/image?url='+encodeURIComponent(src)} alt="" loading="lazy" referrerPolicy="no-referrer" onError={()=>setFailed(true)}/>:<UserRound size={20}/>;}
const types=[['avatars','Avatars'],['users','People'],['worlds','Worlds'],['groups','Groups']] as const;
type Type=typeof types[number][0];
type Row={key:string;name:string;sub:string;image?:string;wide?:boolean;open:()=>void};
/* Pop-up search over the whole app: avatars come from the public catalog, everything else from the VRChat session. */
export default function SearchPalette({close,user,onLogin,onAvatar}:{close:()=>void;user:any;onLogin:()=>void;onAvatar:(a:any)=>void}){useLocale();
 const [type,setType]=useState<Type>('avatars'),[q,setQ]=useState(''),[rows,setRows]=useState<Row[]>([]),[busy,setBusy]=useState(false),[error,setError]=useState(''),[active,setActive]=useState(0);
 const input=useRef<HTMLInputElement>(null),dialog=useRef<HTMLDivElement>(null);
 const needsLogin=type!=='avatars'&&!user;
 useEffect(()=>{const previous=document.activeElement as HTMLElement,old=document.body.style.overflow;document.body.style.overflow='hidden';input.current?.focus();return()=>{document.body.style.overflow=old;previous?.focus?.();};},[]);
 useEffect(()=>{const fn=(e:KeyboardEvent)=>{if(dialog.current!==Array.from(document.querySelectorAll('[role="dialog"]')).at(-1))return;if(e.key==='Escape'){e.stopImmediatePropagation();close();}};document.addEventListener('keydown',fn);return()=>document.removeEventListener('keydown',fn);},[close]);
 useEffect(()=>{const term=q.trim();setError('');setActive(0);if(!term||needsLogin){setRows([]);setBusy(false);return;}let alive=true;setBusy(true);const timer=setTimeout(async()=>{try{
  const go=(kind:Kind,id:string)=>()=>{close();openDetail(kind,id);};
  let next:Row[];
  if(type==='avatars'){const d=await api('/catalog?'+new URLSearchParams({q:term}));next=(d.avatars||[]).slice(0,20).map((a:any)=>({key:a.id,name:a.name,sub:'by '+a.author,image:a.thumbnail,wide:true,open:()=>{close();onAvatar(a);}}));}
  else{const d=await api('/account/search?'+new URLSearchParams({type,q:term,offset:'0'}));const list:any[]=Array.isArray(d)?d:d.results||[];next=list.slice(0,20).map(r=>type==='users'?{key:r.id,name:r.displayName,sub:r.statusDescription||r.status||'VRChat user',image:r.userIcon||r.profilePicOverride||r.currentAvatarThumbnailImageUrl,open:go('user',r.id)}:type==='worlds'?{key:r.id,name:r.name,sub:'by '+r.authorName,image:r.thumbnailImageUrl,wide:true,open:go('world',r.id)}:{key:r.groupId||r.id,name:r.name,sub:[r.shortCode&&r.shortCode+'.'+(r.discriminator||''),(r.memberCount??'?')+' members'].filter(Boolean).join(' · '),image:r.iconUrl,open:go('group',r.groupId||r.id)});}
  if(alive)setRows(next);}catch(e){if(alive){setRows([]);setError((e as Error).message);}}finally{if(alive)setBusy(false);}},250);return()=>{alive=false;clearTimeout(timer);};},[q,type,needsLogin]);
 useEffect(()=>{dialog.current?.querySelector('.palette-row.active')?.scrollIntoView({block:'nearest'});},[active]);
 function keys(e:React.KeyboardEvent){if(e.key==='ArrowDown'&&rows.length){e.preventDefault();setActive((active+1)%rows.length);}else if(e.key==='ArrowUp'&&rows.length){e.preventDefault();setActive((active-1+rows.length)%rows.length);}else if(e.key==='Enter'){e.preventDefault();rows[active]?.open();}else if(e.key==='Tab'&&!e.shiftKey&&document.activeElement===input.current){e.preventDefault();const i=types.findIndex(t=>t[0]===type);setType(types[(i+1)%types.length][0]);}}
 return <div className="modal-backdrop palette-backdrop" onMouseDown={e=>{if(e.target===e.currentTarget)close();}}><div ref={dialog} className="search-palette" role="dialog" aria-modal="true" aria-label={tr("Search")}>
  <div className="palette-input"><Search size={20}/><input ref={input} value={q} onChange={e=>setQ(e.target.value)} onKeyDown={keys} maxLength={100} placeholder={tr('Search {type}…',{type:tr(types.find(t=>t[0]===type)![1])})} aria-label={tr("Search")} role="combobox" aria-expanded={rows.length>0} aria-controls="palette-results" aria-activedescendant={rows[active]?'palette-'+active:undefined}/>{busy?<LoaderCircle className="spin" size={18}/>:q&&<button aria-label={tr("Clear search")} onClick={()=>{setQ('');input.current?.focus();}}><X size={16}/></button>}<button className="palette-esc" onClick={close} aria-label={tr("Close search")}>Esc</button></div>
  <div className="chips palette-types" role="tablist">{types.map(([v,l])=><button key={v} role="tab" aria-selected={type===v} className={type===v?'chosen':''} onClick={()=>{setType(v);input.current?.focus();}}>{tr(l)}</button>)}</div>
  <div className="palette-results" id="palette-results" role="listbox">
   {needsLogin?<div className="palette-empty"><ShieldCheck size={24}/><p>{tr("Connect your VRChat account to search {type}.",{type:tr(types.find(t=>t[0]===type)![1])})}</p><button className="primary" onClick={()=>{close();onLogin();}}>{tr("Connect VRChat")}</button></div>
   :error?<div className="error-banner" role="alert">{error}</div>
   :!q.trim()?<div className="palette-empty"><p>{tr("Type to search. Use ↑ ↓ to move, Enter to open, Tab to switch type.")}</p></div>
   :!busy&&!rows.length?<div className="palette-empty"><p>{tr("No results for “{query}”.",{query:q.trim()})}</p></div>
   :rows.map((r,i)=><button key={r.key} id={'palette-'+i} role="option" aria-selected={i===active} className={'palette-row'+(i===active?' active':'')} onMouseMove={()=>setActive(i)} onClick={r.open}><span className={'palette-thumb'+(r.wide?' wide':'')}><Thumb src={r.image}/></span><span className="palette-copy"><strong>{r.name}</strong><small>{r.sub}</small></span><ArrowUpRight size={15}/></button>)}
  </div>
 </div></div>;
}
