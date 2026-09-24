import test from 'node:test';
import assert from 'node:assert/strict';
import { Window } from 'happy-dom';
import { radarChart } from '../dist/radar.js';
const self = Array.from({length:5}, (_,i)=>({group_id:`g${i}`,name:`Group ${i}`,percent:20+i*10}));
function dom(radar) { const win = new Window(); win.document.body.innerHTML=radarChart(radar); return win.document; }
test('radar overlays two five-point polygons using the same scale and named comparison',()=>{
 const manager=self.map(g=>({...g,percent:g.percent+10}));
 const d=dom({assessment_id:'s',manager_assessment_id:'m',series:{self,manager}});
 const polygons=d.querySelectorAll('polygon.radar-value');
 assert.equal(polygons.length,2);
 assert.equal(polygons[0].getAttribute('points').split(' ').length,5);
 assert.equal(polygons[1].getAttribute('points').split(' ').length,5);
 assert.deepEqual(polygons[0].getAttribute('points').split(' ')[0].split(',').map(Number),[180,138.4]);
 assert.deepEqual(polygons[1].getAttribute('points').split(' ')[0].split(',').map(Number),[180,127.6]);
 assert.match(d.body.textContent,/Самооценка/); assert.match(d.body.textContent,/Менеджер/);
 assert.match(d.body.textContent,/\+10/);
 assert.equal(d.querySelectorAll('.radar-comparison tbody tr').length,5);
});
test('missing manager remains missing rather than an invented zero polygon',()=>{
 const d=dom({assessment_id:'s',series:{self,manager:null}});
 assert.equal(d.querySelectorAll('polygon.radar-value').length,1);
 assert.match(d.body.textContent,/Оценки менеджера пока нет/);
});
test('fewer than three groups uses paired bars and escaped names',()=>{
 const groups=[{group_id:'g',name:'<img src=x>',percent:50}];
 const d=dom({assessment_id:'s',manager_assessment_id:'m',series:{self:groups,manager:[{...groups[0],percent:25}]}});
 assert.equal(d.querySelectorAll('polygon').length,0);
 assert.equal(d.querySelectorAll('progress').length,2);
 assert.equal(d.querySelectorAll('img').length,0);
 assert.match(d.body.textContent,/-25/);
});
test('manager groups follow self axes even if the response order changes',()=>{
 const manager=self.map(g=>({...g,percent:g.percent+10})).reverse();
 const d=dom({series:{self,manager}});
 assert.equal(d.querySelector('.radar-manager').getAttribute('points').split(' ')[0], '180,127.6');
});
