import { escapeHTML as h, percent } from './model.js';
const number = value => new Intl.NumberFormat('ru', {maximumFractionDigits:1}).format(value);
export function radarChart(radar) {
  const groups = radar?.series?.self || [];
  const managerByGroup = new Map((radar?.series?.manager || []).map(g => [g.group_id, g]));
  const manager = groups.map(g => managerByGroup.get(g.group_id));
  const paired = groups.length > 0 && manager.every(Boolean);
  const legend = '<div class="radar-legend"><span class="legend"><i></i>Самооценка</span><span class="legend manager"><i></i>Менеджер</span></div>';
  const note = paired ? 'Полупрозрачные области показывают совпадения и расхождения оценок.' : 'Оценки менеджера пока нет для этой самооценки.';
  if (!groups.length) return `<div class="inline-empty">Сохраните самооценку, чтобы увидеть профиль компетенций.</div>`;
  const table = `<div class="table-wrap"><table class="radar-comparison"><caption>Сравнение по группам · шкала 0–100%</caption><thead><tr><th>Группа</th><th>Самооценка</th><th>Менеджер</th><th>Разница, п.п.</th></tr></thead><tbody>${groups.map((g,i)=>{ const delta=paired ? percent(manager[i].percent)-percent(g.percent):null; return `<tr><th scope="row">${i+1}. ${h(g.name)}</th><td>${number(percent(g.percent))}%</td><td>${paired ? number(percent(manager[i].percent))+'%' : '—'}</td><td>${delta === null ? '—' : (delta > 0 ? '+' : '')+number(delta)}</td></tr>`; }).join('')}</tbody></table></div>`;
  let plot;
  if(groups.length<3) {
    plot=`<div class="group-progress">${groups.map((g,i)=>`<div><strong>${h(g.name)}</strong><label>Самооценка · ${number(percent(g.percent))}%<progress max="100" value="${percent(g.percent)}"></progress></label>${paired ? `<label class="manager">Менеджер · ${number(percent(manager[i].percent))}%<progress max="100" value="${percent(manager[i].percent)}"></progress></label>` : ''}</div>`).join('')}</div>`;
  } else {
    const point=(i,scale)=>[180+Math.sin(i*2*Math.PI/groups.length)*108*scale,160-Math.cos(i*2*Math.PI/groups.length)*108*scale];
    const points=values=>groups.map((g,i)=>point(i,Array.isArray(values)?percent(values[i].percent)/100:values).join(',')).join(' ');
    plot=`<div class="radar radar-overlay"><svg viewBox="0 0 360 330" role="img" aria-label="Роза компетенций: самооценка и оценка менеджера"><title>${h(groups.map((g,i)=>`${g.name}: самооценка ${number(percent(g.percent))}%, менеджер ${paired ? number(percent(manager[i].percent))+'%' : 'нет оценки'}`).join('; '))}</title>${[.25,.5,.75,1].map(scale=>`<polygon points="${points(scale)}" class="radar-grid"/>`).join('')}${groups.map((g,i)=>{const [x,y]=point(i,1);return `<line x1="180" y1="160" x2="${x}" y2="${y}" class="radar-axis"/>`;}).join('')}<polygon points="${points(groups)}" class="radar-value radar-self"/>${paired ? `<polygon points="${points(manager)}" class="radar-value radar-manager"/>` : ''}${groups.map((g,i)=>{const [x,y]=point(i,1.22);return `<text x="${x}" y="${y}" text-anchor="middle" dominant-baseline="middle">${i+1}</text>`;}).join('')}<text x="187" y="58" class="radar-scale">100%</text><text x="187" y="111" class="radar-scale">50%</text></svg></div>`;
  }
  return `${legend}${plot}<p class="radar-note">${note} Цель — 100% на каждой оси.</p>${table}`;
}
