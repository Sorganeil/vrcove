import { useEffect, useRef, useState } from 'react';
import { Check, LoaderCircle, ZoomIn, ZoomOut } from 'lucide-react';
import { t as tr, useLocale } from '../lib/i18n';
import Modal from './modal';

const maxZoom = 8,
  maxOutput = 512;

type Crop = { x: number; y: number; zoom: number };

// Lets the user pick a square part of a picture, then hands back that part as a PNG.
// The browser applies EXIF rotation, so phone photos come out upright.
export default function PictureCropper({
  file,
  close,
  save,
}: {
  file: File;
  close: () => void;
  save: (picture: Blob) => Promise<void>;
}) {
  useLocale();
  const [image, setImage] = useState<HTMLImageElement | null>(null),
    [crop, setCrop] = useState<Crop>({ x: 0, y: 0, zoom: 1 }),
    [error, setError] = useState(''),
    [saving, setSaving] = useState(false),
    view = useRef<HTMLDivElement>(null),
    drag = useRef<{ id: number; x: number; y: number } | null>(null);

  useEffect(() => {
    const url = URL.createObjectURL(file),
      img = new Image();
    img.onload = () => {
      setImage(img);
      setCrop({ x: img.naturalWidth / 2, y: img.naturalHeight / 2, zoom: 1 });
    };
    img.onerror = () => setError(tr('This image could not be read.'));
    img.src = url;
    return () => {
      img.onload = img.onerror = null;
      URL.revokeObjectURL(url);
    };
  }, [file]);

  const width = image?.naturalWidth || 1,
    height = image?.naturalHeight || 1,
    side = Math.min(width, height) / crop.zoom;

  // Keeps the square inside the picture.
  function place(next: Crop) {
    const zoom = Math.min(maxZoom, Math.max(1, next.zoom)),
      half = Math.min(width, height) / zoom / 2;
    setCrop({
      zoom,
      x: Math.min(width - half, Math.max(half, next.x)),
      y: Math.min(height - half, Math.max(half, next.y)),
    });
  }
  const pixels = () => side / (view.current?.clientWidth || 1);

  useEffect(() => {
    const el = view.current;
    if (!el) return;
    const wheel = (e: WheelEvent) => {
      e.preventDefault();
      place({ ...crop, zoom: crop.zoom * Math.exp(-e.deltaY / 400) });
    };
    el.addEventListener('wheel', wheel, { passive: false });
    return () => el.removeEventListener('wheel', wheel);
  });

  async function submit() {
    if (!image) return;
    setSaving(true);
    setError('');
    try {
      const out = Math.max(1, Math.round(Math.min(side, maxOutput))),
        canvas = document.createElement('canvas');
      canvas.width = canvas.height = out;
      const ctx = canvas.getContext('2d')!;
      ctx.imageSmoothingQuality = 'high';
      ctx.drawImage(image, crop.x - side / 2, crop.y - side / 2, side, side, 0, 0, out, out);
      const blob = await new Promise<Blob | null>((done) => canvas.toBlob(done, 'image/png'));
      if (!blob) throw new Error(tr('This image could not be read.'));
      await save(blob);
    } catch (e) {
      setError((e as Error).message);
      setSaving(false);
    }
  }

  const scale = 100 / side;
  return (
    <Modal title={tr('Crop picture')} close={saving ? () => {} : close}>
      <div className="login-content picture-cropper">
        <div className="eyebrow">{tr('PROFILE PICTURE')}</div>
        <h2>{tr('Crop picture')}</h2>
        <p className="fineprint">{tr('Drag to move. Zoom to choose how much is shown.')}</p>
        <div
          ref={view}
          className="crop-view"
          tabIndex={0}
          role="application"
          aria-label={tr('Picture crop area. Use arrow keys to move and plus or minus to zoom.')}
          onPointerDown={(e) => {
            e.currentTarget.setPointerCapture(e.pointerId);
            drag.current = { id: e.pointerId, x: e.clientX, y: e.clientY };
          }}
          onPointerMove={(e) => {
            const d = drag.current;
            if (!d || d.id !== e.pointerId) return;
            const k = pixels();
            place({ ...crop, x: crop.x - (e.clientX - d.x) * k, y: crop.y - (e.clientY - d.y) * k });
            drag.current = { ...d, x: e.clientX, y: e.clientY };
          }}
          onPointerUp={() => (drag.current = null)}
          onPointerCancel={() => (drag.current = null)}
          onKeyDown={(e) => {
            const step = side / 20,
              moves: Record<string, Partial<Crop>> = {
                ArrowLeft: { x: crop.x - step },
                ArrowRight: { x: crop.x + step },
                ArrowUp: { y: crop.y - step },
                ArrowDown: { y: crop.y + step },
                '+': { zoom: crop.zoom * 1.1 },
                '=': { zoom: crop.zoom * 1.1 },
                '-': { zoom: crop.zoom / 1.1 },
              };
            if (!moves[e.key]) return;
            e.preventDefault();
            place({ ...crop, ...moves[e.key] });
          }}
        >
          {image ? (
            <img
              src={image.src}
              alt=""
              draggable={false}
              style={{
                left: (0.5 - crop.x / side) * 100 + '%',
                top: (0.5 - crop.y / side) * 100 + '%',
                width: width * scale + '%',
                height: height * scale + '%',
              }}
            />
          ) : (
            !error && <LoaderCircle className="spin" />
          )}
        </div>
        <label className="crop-zoom">
          <ZoomOut size={16} aria-hidden="true" />
          <input
            type="range"
            min={1}
            max={maxZoom}
            step={0.01}
            value={crop.zoom}
            aria-label={tr('Zoom')}
            disabled={!image || saving}
            onChange={(e) => place({ ...crop, zoom: Number(e.target.value) })}
          />
          <ZoomIn size={16} aria-hidden="true" />
        </label>
        {error && (
          <div className="form-error" role="alert">
            {error}
          </div>
        )}
        <button type="button" className="primary" disabled={!image || saving} onClick={submit}>
          {saving ? <LoaderCircle size={16} className="spin" /> : <Check size={16} />}
          {tr('Use this picture')}
        </button>
        <button type="button" className="standard-button" onClick={close} disabled={saving}>
          {tr('Cancel')}
        </button>
      </div>
    </Modal>
  );
}
