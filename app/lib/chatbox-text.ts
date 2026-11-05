// The browser's copy of companion/internal/chatbox's rendering, so the preview
// matches what reaches VRChat. Keep the two in step.
export type ChatboxValues = Partial<Record<string, string>>;
export const chatboxLimit = 144;
const lineLimit = 9;

function closeGaps(line: string) {
  for (const gap of [' | ', ' · ', ' • ', ' - ', ' / ']) {
    while (line.includes(gap + gap)) line = line.replaceAll(gap + gap, gap);
  }
  return line.replace(/^[ |·•\-/,:]+|[ |·•\-/,:]+$/g, '');
}

export function fitChatbox(text: string) {
  const lines = text.split('\n');
  if (lines.length > lineLimit) text = lines.slice(0, lineLimit).join('\n');
  if (text.length <= chatboxLimit) return text;
  let cut = '';
  for (const character of text) {
    if (cut.length + character.length > chatboxLimit - 1) break;
    cut += character;
  }
  const space = Math.max(cut.lastIndexOf(' '), cut.lastIndexOf('\n'));
  if (space > 0 && space > cut.length - 24) cut = cut.slice(0, space);
  return cut.trimEnd() + '…';
}

export function renderChatbox(template: string, values: ChatboxValues) {
  const lines: string[] = [];
  for (const line of template.split('\n')) {
    let filled = 0;
    let empty = 0;
    const text = line.replace(/\{(\w+)\}/g, (match, key: string) => {
      if (!(key in values)) return match;
      if (values[key]) filled++;
      else empty++;
      return values[key] || '';
    });
    if (filled === 0 && empty > 0) continue;
    lines.push(empty > 0 ? closeGaps(text) : text);
  }
  return fitChatbox(lines.join('\n'));
}
