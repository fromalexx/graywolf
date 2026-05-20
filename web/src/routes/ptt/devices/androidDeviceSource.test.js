// web/src/routes/ptt/devices/androidDeviceSource.test.js
import { test, before, after } from 'node:test';
import assert from 'node:assert/strict';

before(() => {
  globalThis.window = globalThis.window ?? { location: { hash: '' } };
});

test('androidDeviceSource.list filters /api/ptt/available by method deviceClass', async () => {
  const fakeApi = {
    get: async (path) => {
      assert.equal(path, '/ptt/available');
      return [
        { path: '', name: 'CP2102N', type: 'usb-cp2102n', usb_vendor: '10c4', usb_product: 'ea60', recommended: true, has_permission: true },
        { path: '', name: 'AIOC',    type: 'usb-cdc-acm',  usb_vendor: '1209', usb_product: '7388', recommended: true, has_permission: false },
        { path: '', name: 'CM108',   type: 'usb-hid',      usb_vendor: '0d8c', usb_product: '013c', recommended: true, has_permission: true },
        { path: '', name: 'Other',   type: 'usb-other',    usb_vendor: 'ffff', usb_product: '0000', recommended: false },
      ];
    },
  };
  const { createAndroidDeviceSource } = await import('./androidDeviceSource.js');
  const src = createAndroidDeviceSource(fakeApi);

  const cp = await src.list({ wire: { method: 'android', ppt_method: 1 }, deviceClass: 'cp2102n' });
  assert.deepEqual(cp.map(d => d.name), ['CP2102N']);

  const aioc = await src.list({ wire: { method: 'android', ppt_method: 3 }, deviceClass: 'cdc-acm' });
  assert.deepEqual(aioc.map(d => d.name), ['AIOC']);

  const cm = await src.list({ wire: { method: 'android', ppt_method: 2 }, deviceClass: 'hid-cm108' });
  assert.deepEqual(cm.map(d => d.name), ['CM108']);
});

test('androidDeviceSource.list for VOX (no deviceClass) returns an empty array', async () => {
  const fakeApi = { get: async () => [{ name: 'CP2102N', type: 'usb-cp2102n', recommended: true }] };
  const { createAndroidDeviceSource } = await import('./androidDeviceSource.js');
  const src = createAndroidDeviceSource(fakeApi);
  assert.deepEqual(await src.list({ wire: { method: 'android', ppt_method: 4 } }), []);
});

test('androidDeviceSource.requestPermission calls the JS bridge and resolves to the granted boolean', async () => {
  const calls = [];
  globalThis.GraywolfWebInterface = {
    requestUsbPermission(vid, pid, callbackId) {
      calls.push({ vid, pid, callbackId });
      // Simulate Kotlin's evaluateJavascript("__usbResult(id, true)") call.
      setTimeout(() => { globalThis.__usbResult(callbackId, true); }, 0);
    },
  };

  const { createAndroidDeviceSource } = await import('./androidDeviceSource.js');
  const src = createAndroidDeviceSource({ get: async () => [] });

  const granted = await src.requestPermission({ usb_vendor: '10c4', usb_product: 'ea60' });
  assert.equal(granted, true);
  assert.equal(calls.length, 1);
  assert.equal(calls[0].vid, 0x10c4);
  assert.equal(calls[0].pid, 0xea60);
});

after(() => {
  delete globalThis.GraywolfWebInterface;
  delete globalThis.__usbResult;
  delete globalThis.__usbCallbacks;
});
