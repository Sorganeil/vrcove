import {entityPath,covePath,appLink,goTo,openQuickView,type EntityKind} from './routes';
import {t as tr,useLocale} from './i18n';
import {useState,useEffect,useRef} from 'react';
import {LoaderCircle,Library,Lock,Globe2,Plus,Check,Trash2,Copy,Pencil,ChevronLeft,UserRound,Star,Users,X,FolderPlus} from 'lucide-react';
import {CovePicture,api,day,Stars,ApiError} from './cove-account';
type CollectionKind='avatar'|'world';
type Summary={id:string;kind:CollectionKind;name:string;description:string;public:boolean;updated:string;count:number;contains:boolean;preview:string[]};
const kindLabel:Record<string,string>={user:'User',world:'World',avatar:'Avatar',group:'Group'};
/* Where a reviewed or collected VRChat item opens in this app. */
const targetHref=(kind:string,id:string)=>entityPath(kind as EntityKind,id);
// A plain click opens the quick view, as elsewhere; modified clicks open the full page.
const targetLink=(kind:string,id:string)=>({href:targetHref(kind,id),onClick:(e:React.MouseEvent)=>{if(e.metaKey||e.ctrlKey||e.shiftKey||e.altKey||e.button)return;e.preventDefault();openQuickView(kind as EntityKind,id);}});
function Thumb({url,alt=''}:{url?:string;alt?:string}){useLocale();const [fail,setFail]=useState(false);return url&&!fail?<img src={'/api/image?url='+encodeURIComponent(url)} alt={alt} onError={()=>setFail(true)} referrerPolicy="no-referrer" loading="lazy"/>:<div className="image-fallback"><Library strokeWidth={.8}/></div>;}
function Back(){useLocale();return <div className="detail-toolbar"><button className="text-link" onClick={()=>window.history.back()}><ChevronLeft size={14}/>{tr("Back")}</button></div>;}
function useResource(path:string){
 const [state,setState]=useState<{path:string;data:any;error:string;status:number}>({path:'',data:null,error:'',status:0});
 const [version,setVersion]=useState(0);
 useEffect(()=>{let alive=true;api(path).then(data=>{if(alive)setState({path,data,error:'',status:200});}).catch((e:ApiError)=>{if(alive)setState({path,data:null,error:e.message,status:e.status});});return()=>{alive=false;};},[path,version]);
 const current=state.path===path?state:{path,data:null,error:'',status:0};
 return {...current,reload:()=>setVersion(v=>v+1)};
}
function CollectionCard({c}:{c:Summary}){useLocale();return <a className="entity-card collection-card" {...appLink(covePath('collection',c.id))}><div className={'entity-card-image wide collection-mosaic n'+Math.min(c.preview.length,4)}>{c.preview.length?c.preview.slice(0,4).map((u,i)=><Thumb key={i} url={u}/>):<div className="image-fallback"><Library strokeWidth={.8}/></div>}</div><strong>{c.name}{!c.public&&<Lock size={12} aria-label={tr("Private")}/>}</strong><small>{tr(c.count===1?"1 item":"{count} items",{count:c.count})} {tr("· updated")} {day(c.updated)}</small></a>;}

/* /cove/<username>: a VRCove profile, visible to everyone when public. */
export function ProfilePage({username}:{username:string}){useLocale();
 const profile=useResource('/cove/profile?username='+encodeURIComponent(username));
 const [more,setMore]=useState<any[]>([]),[offset,setOffset]=useState(0),[loadingMore,setLoadingMore]=useState(false),[moreError,setMoreError]=useState('');
 useEffect(()=>{setMore([]);setOffset(0);},[username]);
 const p=profile.data;
 if(profile.error)return <div className="entity-page"><Back/><div className="empty-state"><Lock size={30}/><h3>{profile.status===404?tr("Profile unavailable"):tr("Couldn’t load this profile")}</h3><p>{profile.error}</p></div></div>;
 if(!p)return <div className="loading-state"><LoaderCircle className="spin"/>{tr("Loading profile…")}</div>;
 async function loadMore(){if(loadingMore)return;setLoadingMore(true);setMoreError('');try{const n=offset+p.pageSize;const d=await api('/cove/profile?username='+encodeURIComponent(username)+'&offset='+n);setMore(m=>[...m,...d.reviews]);setOffset(n);}catch(e){setMoreError((e as Error).message);}finally{setLoadingMore(false);}}
 const reviews=[...p.reviews,...more];
 return <div className="entity-page cove-profile"><Back/>
  <div className="cove-profile-hero"><CovePicture picture={p.picture} username={p.username} size={72}/><div><div className="eyebrow"><UserRound size={13}/>{tr("VRCOVE PROFILE")}{p.public?'':' / PRIVATE'}</div><h1>{p.username}</h1>{p.status&&<p className="cove-profile-status">{p.status}</p>}<p className="creator">{tr("Member since")} {day(p.created)} · {tr(p.reviewCount===1?"1 review":"{count} reviews",{count:p.reviewCount})}{p.reviewCount>0&&<> {tr("· average given")} {p.averageGiven.toFixed(1)}<Star size={12}/></>}</p></div></div>
  {p.mine&&<a className="standard-button cove-profile-edit" {...appLink('/account')}><Pencil size={14}/>{tr("Edit VRCove profile")}</a>}
  {p.mine&&!p.public&&<div className="privacy-note"><Lock size={18}/><p>{tr("Only you can see this page. Make your profile public in")} <a className="text-link" {...appLink('/account')}>{tr("your VRCove account")}</a> {tr("to share it.")}</p></div>}
  {p.bio&&<section className="entity-section cove-about"><h3>{tr("About")}</h3><p className="description cove-bio">{p.bio}</p></section>}
  <section className="entity-section"><h3><Library size={16}/>{tr("Collections")}{p.collections.length>0&&<small>{p.collections.length}</small>}</h3>{p.collections.length?<div className="entity-grid">{p.collections.map((c:Summary)=><CollectionCard key={c.id} c={c}/>)}</div>:<p className="fineprint">{p.mine?<>{tr("No collections yet.")} <a className="text-link" {...appLink('/collections')}>{tr("Make one")}</a>.</>:tr("No public collections.")}</p>}</section>
  <section className="entity-section reviews"><h3><Star size={16}/>{tr("Reviews")}{p.reviewCount>0&&<small>{p.reviewCount}</small>}</h3>
   {reviews.length?<div className="review-list">{reviews.map((r:any)=><article className="review" key={r.kind+r.target}><header><span className="review-kind">{tr(kindLabel[r.kind])}</span><a className="review-author" {...targetLink(r.kind,r.target)}>{r.name||r.target}</a><Stars value={r.rating}/><small>{day(r.date)}{r.edited?' · edited':''}</small></header>{r.body&&<p>{r.body}</p>}</article>)}</div>:<p className="fineprint">{tr("No reviews yet.")}</p>}
   {moreError&&<p className="form-error" role="alert">{moreError}</p>}{reviews.length<p.reviewCount&&<button className="standard-button" disabled={loadingMore} onClick={loadMore}>{loadingMore?tr("Loading…"):tr("Load more")}</button>}
  </section>
 </div>;
}

function CollectionForm({initial,kindLocked,busy,submit,cancel}:{initial?:Partial<Summary>;kindLocked?:boolean;busy:boolean;submit:(v:{kind:CollectionKind;name:string;description:string;public:boolean})=>void;cancel?:()=>void}){useLocale();
 const [kind,setKind]=useState<CollectionKind>(initial?.kind||'avatar'),[name,setName]=useState(initial?.name||''),[description,setDescription]=useState(initial?.description||''),[pub,setPub]=useState(!!initial?.public);
 return <form className="cove-panel login-content collection-form" onSubmit={e=>{e.preventDefault();submit({kind,name,description,public:pub});}}>
  {!kindLocked&&<div className="chips" role="radiogroup" aria-label={tr("Collection type")}><button type="button" role="radio" aria-checked={kind==='avatar'} className={kind==='avatar'?'chosen':''} onClick={()=>setKind('avatar')}>{tr("Avatars")}</button><button type="button" role="radio" aria-checked={kind==='world'} className={kind==='world'?'chosen':''} onClick={()=>setKind('world')}>{tr("Worlds")}</button></div>}
  <label>{tr("Name")}<input value={name} onChange={e=>setName(e.target.value)} required maxLength={80} placeholder={kind==='avatar'?'e.g. Quest-friendly favourites':'e.g. Chill worlds for late nights'}/></label>
  <label>{tr("Description")}<textarea value={description} onChange={e=>setDescription(e.target.value)} maxLength={1000} rows={3} placeholder={tr("Optional")}/></label>
  <label className="cove-switch"><input type="checkbox" checked={pub} onChange={e=>setPub(e.target.checked)}/><span>{tr("Public: anyone with the link can view it")}{' '}{tr("and it’s listed on your profile if your profile is public")}</span></label>
  <div className="result-actions"><button className="primary" type="submit" disabled={busy}>{busy?<LoaderCircle size={14} className="spin"/>:<Check size={14}/>}{initial?.id?tr("Save"):'Create collection'}</button>{cancel&&<button type="button" className="standard-button" onClick={cancel}>{tr("Cancel")}</button>}</div>
 </form>;
}

/* #collections: the signed-in account's own collections. */
export function CollectionsPage({notify}:{notify:(s:string)=>void}){useLocale();
 const list=useResource('/cove/collections');
 const [creating,setCreating]=useState(false),[busy,setBusy]=useState(false),[error,setError]=useState('');
 const heading=<div className="section-heading"><div><h2>{tr("My collections")}</h2><p>{tr("Group avatars or worlds into lists. Keep them private or share them by link.")}</p></div>{list.data&&!creating&&<button className="primary" onClick={()=>setCreating(true)}><Plus size={15}/>{tr("New collection")}</button>}</div>;
 async function create(v:any){setBusy(true);setError('');try{const r=await api('/cove/collections/create',v);notify(tr("Collection created"));goTo(covePath('collection',r.id));}catch(e){setError((e as Error).message);}finally{setBusy(false);}}
 if(list.status===401||list.status===403||list.status===503)return <section className="catalog-section">{heading}<div className="empty-state"><Library size={30}/><h3>{list.status===401?'Sign in to make collections':list.status===403?'Link a VRChat account':'Accounts aren’t enabled here'}</h3><p>{list.status===401?'Collections belong to your VRCove account, not your VRChat account.':list.error}</p>{(list.status===401||list.status===403)&&<a className="primary" {...appLink('/account')}>{tr("Sign in to VRCove")}</a>}</div></section>;
 return <section className="catalog-section">{heading}
  {creating&&<CollectionForm busy={busy} submit={create} cancel={()=>setCreating(false)}/>}
  {error&&<div className="form-error" role="alert">{error}</div>}
  {list.error?<div className="error-banner" role="alert">{list.error}</div>:!list.data?<div className="loading-state"><LoaderCircle className="spin"/>{tr("Loading…")}</div>:list.data.length?<div className="entity-grid collection-grid">{list.data.map((c:Summary)=><CollectionCard key={c.id} c={c}/>)}</div>:!creating&&<div className="empty-state"><Library size={30}/><h3>{tr("No collections yet")}</h3><p>{tr("Create one here, or use “Add to collection” on any avatar or world.")}</p></div>}
 </section>;
}

/* /collection/<id>: one collection, for its owner or anyone if public. */
export function CollectionPage({id,notify}:{id:string;notify:(s:string)=>void}){useLocale();
 const col=useResource('/cove/collection?id='+encodeURIComponent(id));
 const [editing,setEditing]=useState(false),[confirmDelete,setConfirmDelete]=useState(false),[busy,setBusy]=useState(''),[error,setError]=useState('');
 const c=col.data;
 if(col.error)return <div className="entity-page"><Back/><div className="empty-state"><Lock size={30}/><h3>{tr("Collection unavailable")}</h3><p>{col.error}</p></div></div>;
 if(!c)return <div className="loading-state"><LoaderCircle className="spin"/>{tr("Loading collection…")}</div>;
 async function run(name:string,fn:()=>Promise<void>){setBusy(name);setError('');try{await fn();}catch(e){setError((e as Error).message);}finally{setBusy('');}}
 const save=(v:any)=>run('save',async()=>{await api('/cove/collections/update',{id,name:v.name,description:v.description,public:v.public});setEditing(false);col.reload();notify(tr("Collection saved"));});
 const remove=(target:string)=>run('remove:'+target,async()=>{await api('/cove/collections/remove',{id,target});col.reload();});
 const destroy=()=>run('delete',async()=>{await api('/cove/collections/delete',{id});notify(tr("Collection deleted"));goTo('/collections');});
 const copy=()=>navigator.clipboard.writeText(window.location.origin+'/collection/'+id).then(()=>notify(tr("Link copied"))).catch(()=>notify(tr("Clipboard access is unavailable.")));
 return <div className="entity-page"><Back/>
  {editing?<CollectionForm initial={c} kindLocked busy={busy==='save'} submit={save} cancel={()=>setEditing(false)}/>:<div className="collection-head">
   <div className="eyebrow">{c.public?<Globe2 size={13}/>:<Lock size={13}/>}{c.kind==='avatar'?'AVATAR':tr("WORLD")} {tr("COLLECTION")}{c.public?'':' / PRIVATE'}</div>
   <h1>{c.name}</h1>
   <p className="creator">{c.owner?<>{tr("by")} <a {...appLink(covePath('cove',c.owner))}>{c.owner}</a> · </>:c.mine?'':'by a VRCove user · '}{c.items.length} {c.kind}{c.items.length===1?'':'s'} {tr("· updated")} {day(c.updated)}</p>
   {c.description&&<p className="description cove-bio">{c.description}</p>}
   <div className="result-actions">{c.public&&<button className="standard-button" onClick={copy}><Copy size={13}/>{tr("Copy link")}</button>}{c.mine&&<><button className="standard-button" onClick={()=>setEditing(true)}><Pencil size={13}/>{tr("Edit")}</button>{confirmDelete?<><button className="standard-button danger-text" disabled={!!busy} onClick={destroy}>{busy==='delete'?<LoaderCircle size={13} className="spin"/>:<Trash2 size={13}/>}{tr("Delete for good")}</button><button className="standard-button" onClick={()=>setConfirmDelete(false)}>{tr("Keep it")}</button></>:<button className="standard-button danger-text" onClick={()=>setConfirmDelete(true)}><Trash2 size={13}/>{tr("Delete")}</button>}</>}</div>
  </div>}
  {error&&<div className="form-error" role="alert">{error}</div>}
  {c.items.length?<div className={'entity-grid collection-items '+c.kind}>{c.items.map((it:any)=><div className="collection-item" key={it.id}><a className="entity-card" {...targetLink(c.kind,it.id)}><div className={'entity-card-image'+(c.kind==='world'?' wide':' tall')}><Thumb url={it.image} alt=""/></div><strong>{it.name||it.id}</strong><small>{tr("Added")} {day(it.added)}</small></a>{c.mine&&<button className="icon-button collection-remove" aria-label={'Remove '+(it.name||it.id)} disabled={!!busy} onClick={()=>remove(it.id)}>{busy==='remove:'+it.id?<LoaderCircle size={14} className="spin"/>:<X size={14}/>}</button>}</div>)}</div>:<div className="empty-state"><Library size={30}/><h3>{tr("Nothing here yet")}</h3><p>{c.mine?'Use “Add to collection” on any '+c.kind+' to add it.':tr("This collection is empty.")}</p></div>}
 </div>;
}

/* Button plus dialog that toggles an avatar or world in the user's collections. */
export function AddToCollection({kind,target,name,image,className='standard-button'}:{kind:CollectionKind;target:string;name:string;image?:string;className?:string}){useLocale();
 const [open,setOpen]=useState(false);
 return <>{<button type="button" className={className} onClick={()=>setOpen(true)}><FolderPlus size={16}/>{tr("Add to collection")}</button>}{open&&<CollectionDialog kind={kind} target={target} name={name} image={image} close={()=>setOpen(false)}/>}</>;
}
function CollectionDialog({kind,target,name,image,close}:{kind:CollectionKind;target:string;name:string;image?:string;close:()=>void}){useLocale();
 const list=useResource('/cove/collections?kind='+kind+'&contains='+encodeURIComponent(target));
 const [busy,setBusy]=useState(''),[error,setError]=useState(''),[newName,setNewName]=useState(''),[changed,setChanged]=useState<Record<string,boolean>>({}),[status,setStatus]=useState('');
 const ref=useRef<HTMLDivElement>(null);
 useEffect(()=>{const previous=document.activeElement as HTMLElement,old=document.body.style.overflow;document.body.style.overflow='hidden';ref.current?.focus();return()=>{document.body.style.overflow=old;previous?.focus();};},[]);
 function dialogKey(e:React.KeyboardEvent){
  if(e.key==='Escape'){e.stopPropagation();e.nativeEvent.stopImmediatePropagation();close();}
  if(e.key==='Tab'){const nodes=Array.from(ref.current?.querySelectorAll<HTMLElement>('button:not(:disabled),a[href],input:not(:disabled)')||[]),first=nodes[0],last=nodes.at(-1);if(e.shiftKey&&(document.activeElement===first||document.activeElement===ref.current)){e.preventDefault();last?.focus();}else if(!e.shiftKey&&(document.activeElement===last||document.activeElement===ref.current)){e.preventDefault();first?.focus();}}
 }
 const item={target,name:name.slice(0,120),image:image||''};
 async function toggle(c:Summary){const has=changed[c.id]??c.contains;setBusy(c.id);setError('');setStatus('');try{await api('/cove/collections/'+(has?'remove':'add'),{id:c.id,...item});setChanged(m=>({...m,[c.id]:!has}));setStatus(tr(has?'Removed from collection.':'Added to collection.'));}catch(e){setError((e as Error).message);}finally{setBusy('');}}
 async function create(e:React.FormEvent){e.preventDefault();setBusy('new');setError('');setStatus('');try{const r=await api('/cove/collections/create',{kind,name:newName,description:'',public:false});await api('/cove/collections/add',{id:r.id,...item});setNewName('');setChanged(m=>({...m,[r.id]:true}));setStatus(tr('Added to collection.'));list.reload();}catch(e){setError((e as Error).message);}finally{setBusy('');}}
 return <div className="modal-backdrop" onMouseDown={e=>{if(e.target===e.currentTarget)close();}}><div ref={ref} tabIndex={-1} className="modal" role="dialog" aria-modal="true" aria-label={tr("Add to collection")} onKeyDown={dialogKey}>
  <button className="modal-close" aria-label={tr("Close dialog")} onClick={close}><X size={20}/></button>
  <div className="login-content"><div className="eyebrow"><Library size={13}/>{kind==='avatar'?'AVATAR':tr("WORLD")} {tr("COLLECTIONS")}</div><h2>{tr("Add to collection")}</h2><p>{tr("Choose a collection for {name}.",{name})}</p><p className="fineprint">{tr('Select a collection to add this item. Changes are saved immediately.')}</p>
   {(list.status===401||list.status===403)?<div className="empty-state"><Users size={26}/><h3>{tr(list.status===403?"Link a VRChat account":"Sign in to VRCove")}</h3><p>{tr("Sign in to VRCove, then link VRChat to use collections.")}</p><a className="primary" {...appLink('/account',close)}>{tr("Sign in")}</a></div>:list.error?<div className="form-error">{list.error}</div>:!list.data?<div className="loading-inline"><LoaderCircle size={14} className="spin"/>{tr("Loading…")}</div>:<>
    {list.data.length>0&&<ul className="collection-choices">{list.data.map((c:Summary)=>{const has=changed[c.id]??c.contains;return <li key={c.id}><button type="button" role="checkbox" aria-checked={has} disabled={!!busy} onClick={()=>toggle(c)}><span className={'check'+(has?' on':'')}>{busy===c.id?<LoaderCircle size={13} className="spin"/>:has&&<Check size={13}/>}</span><span><strong>{c.name}</strong><small>{c.count+((changed[c.id]!==undefined&&changed[c.id]!==c.contains)?(has?1:-1):0)} {tr("items")}{c.public?'':' · private'}</small></span></button></li>;})}</ul>}
    <p className="fineprint">{tr("Or create a new collection (optional)")}</p>
    <form className="collection-quick" onSubmit={create}><input value={newName} onChange={e=>setNewName(e.target.value)} maxLength={80} required placeholder={tr("New collection")} aria-label={tr("New collection name")}/><button className="standard-button" type="submit" disabled={!!busy||!newName.trim()}>{busy==='new'?<LoaderCircle size={14} className="spin"/>:<Plus size={14}/>}{tr("Create & add")}</button></form>
    <p className="fineprint">{tr("New collections start private.")} <a className="text-link" {...appLink('/collections',close)}>{tr("Manage collections")}</a></p>
   </>}
   {error&&<div className="form-error" role="alert">{error}</div>}
   {status&&<p className="collection-save-status" role="status"><Check size={15}/>{status}</p>}
   <div className="result-actions"><button type="button" className="primary" disabled={!!busy} onClick={close}>{tr("Done")}</button></div>
  </div></div></div>;
}
