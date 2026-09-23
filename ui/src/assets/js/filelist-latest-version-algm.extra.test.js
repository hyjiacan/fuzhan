/**
 * 补充边界用例测试：覆盖 docs「…自动识别与高亮算法文档」第 7 章用例集，
 * 以及集成为真实 file 对象输入时的场景。
 *
 * 运行方式：
 *   node --test --test-reporter=spec filelist-latest-version-algm.extra.test.js
 */
import { describe, it } from 'node:test';
import assert from 'node:assert/strict';
import latestVersionModule from './filelist-latest-version-algm.js';

const { analyzeLatestVersions } = latestVersionModule;

function latestOf(files, config = {}) {
  const list = files.map((p, i) =>
    typeof p === 'object'
      ? p
      : { path: p, name: p.split('/').pop(), type: 'file', modifiedTime: new Date(2026, 0, 1, 0, i).toISOString() }
  );
  const r = analyzeLatestVersions(list, config);
  return Array.from(r.latestPaths).map((p) => p.split('/').pop()).sort();
}

describe('文档第 7 章边界用例集', () => {
  it('点分多段：app_v2.0 胜出', () => {
    assert.deepStrictEqual(latestOf(['app_v1.2.3.exe', 'app_v2.0.exe']), ['app_v2.0.exe']);
  });
  it('点分含预发布：稳定版优先', () => {
    assert.deepStrictEqual(latestOf(['app-1.2.3-beta.1.exe', 'app-1.2.3.exe']), ['app-1.2.3.exe']);
  });
  it('数值比较：12.1 > 2.1', () => {
    assert.deepStrictEqual(latestOf(['app-2.1.exe', 'app-12.1.exe']), ['app-12.1.exe']);
  });
  it('短版本补零：1.2 与 1.2.0 视为相等，回退修改时间取新者', () => {
    const files = [
      { path: '/a/app-1.2.zip', name: 'app-1.2.zip', type: 'file', modifiedTime: '2026-01-01T00:00:00Z' },
      { path: '/a/app-1.2.0.zip', name: 'app-1.2.0.zip', type: 'file', modifiedTime: '2026-01-02T00:00:00Z' }
    ];
    assert.deepStrictEqual(latestOf(files), ['app-1.2.0.zip']);
  });
  it('尾部整数：app-10.zip 胜出', () => {
    assert.deepStrictEqual(latestOf(['app-2.zip', 'app-10.zip']), ['app-10.zip']);
  });
  it('日期：2024-01-02 > 20240101', () => {
    assert.deepStrictEqual(latestOf(['backup-20240101.zip', 'backup-2024-01-02.zip']), ['backup-2024-01-02.zip']);
  });
  it('非法日期：不识别为日期，降级为普通数字比较', () => {
    // 文档第 7 章：非法日期（如 20241345）不识别为日期 → 版本类型降级为 NUM，
    // 与同为 NUM 的名字按数值比较（20241400 > 20241300）
    assert.deepStrictEqual(
      latestOf(['backup-20241300.zip', 'backup-20241400.zip']),
      ['backup-20241400.zip']
    );
    // 单个非法日期名字被解析为 NUM，而非 DATE
    const pf = latestVersionModule.parseFile('backup-20241345.zip', {});
    assert.strictEqual(pf.versionType, 'NUM');
  });
  it('括号序号：report(2) 胜出', () => {
    assert.deepStrictEqual(latestOf(['report.pdf', 'report(1).pdf', 'report(2).pdf']), ['report(2).pdf']);
  });
  it('v 前缀：v1.10.0 > v1.2.3', () => {
    assert.deepStrictEqual(latestOf(['v1.2.3.zip', 'v1.10.0.zip']), ['v1.10.0.zip']);
  });
  it('主体含版本：pandoc 3.11.1 > 3.11', () => {
    assert.deepStrictEqual(latestOf(['pandoc-3.11-windows-x86_64.zip', 'pandoc-3.11.1-windows-x86_64.zip']), ['pandoc-3.11.1-windows-x86_64.zip']);
  });
  it('通道 merge 默认：跨通道取最高版本', () => {
    assert.deepStrictEqual(latestOf(['chrome-1.2.3-stable.exe', 'chrome-1.3.0-beta.exe']), ['chrome-1.3.0-beta.exe']);
  });
  it('通道 separate：各通道独立，孤例默认过滤', () => {
    assert.deepStrictEqual(
      latestOf(['chrome-1.2.3-stable.exe', 'chrome-1.3.0-beta.exe'], { channelGrouping: 'separate' }),
      []
    );
  });
  it('平台 rank：无标记文件优先', () => {
    assert.deepStrictEqual(latestOf(['app-1.0.0-win.exe', 'app-1.0.0-mac.dmg', 'app-1.0.0.exe']), ['app-1.0.0.exe']);
  });
  it('平台内判定：不同扩展名独立取最新，不为跨平台剔除 linux', () => {
    // linux 的 .tar.gz 独立成产物组，1.26.4 应被标出；msi 组标 1.26.3；zip 组孤例不标
    const files = [
      'go1.21.6.linux-amd64.tar.gz',
      'go1.21.6.windows-amd64.msi',
      'go1.26.3.windows-amd64.msi',
      'go1.26.4.linux-amd64.tar.gz'
    ];
    assert.deepStrictEqual(
      latestOf(files),
      ['go1.26.3.windows-amd64.msi', 'go1.26.4.linux-amd64.tar.gz']
    );
  });
  it('复合架构词：x86_64 归一为 64 位，优先于 i686 且按日期取最新', () => {
    // x86_64 若被拆成 x86+64 会误判为 32 位；应整词归一为 x64
    const files = ['msys2-i686-20190524.exe', 'msys2-x86_64-20210419.exe', 'msys2-x86_64-20260322.exe'];
    assert.deepStrictEqual(latestOf(files), ['msys2-x86_64-20260322.exe']);
  });
  it('复合架构词连字符变体：app-x86-64-1.2.exe 视为 64 位', () => {
    assert.deepStrictEqual(
      latestOf(['app-x86-64-1.1.exe', 'app-x86-64-1.2.exe']),
      ['app-x86-64-1.2.exe']
    );
  });
  it('架构 separate：x64 与 x86 各选最新', () => {
    const files = ['app-1.0-x64.exe', 'app-2.0-x64.exe', 'app-1.0-x86.exe', 'app-2.0-x86.exe'];
    assert.deepStrictEqual(
      latestOf(files, { archGrouping: 'separate' }),
      ['app-2.0-x64.exe', 'app-2.0-x86.exe']
    );
  });
  it('发布形态：official/offline 各自孤例，默认不高亮', () => {
    assert.deepStrictEqual(latestOf(['app_official.zip', 'app_offline.exe']), []);
  });
  it('双扩展名：tar.gz 作为整体扩展', () => {
    assert.deepStrictEqual(latestOf(['app-1.0.tar.gz', 'app-2.0.tar.gz']), ['app-2.0.tar.gz']);
  });
  it('主体含数字：x64studio / office2021 不被误剥，各自孤例', () => {
    assert.deepStrictEqual(latestOf(['x64studio.7z', 'office2021.zip']), []);
  });
  it('孤例：app-3.0.exe 单独存在不标记', () => {
    assert.deepStrictEqual(latestOf(['app-3.0.exe']), []);
  });
  it('无版本号文件：同族多文件均无版本也不标记', () => {
    assert.deepStrictEqual(latestOf(['app.exe', 'app.bin']), []);
  });
  it('无版本号文件：单文件也不标记', () => {
    assert.deepStrictEqual(latestOf(['app.exe']), []);
  });
  it('混合同族：含版本者胜出，无版本文件不夺标', () => {
    // 与 "app-1.0.exe" 同族时，无版本号的 app.exe 不参与，标 1.0
    assert.deepStrictEqual(latestOf(['app.exe', 'app-1.0.exe', 'app.bin']), ['app-1.0.exe']);
  });
  it('跨扩展名单份：同签名但扩展名只有一份时不标记', () => {
    // Firefox 场景：Setup 下有 .msi 与 .exe 各一份，各自无同类 → 均不标
    assert.deepStrictEqual(
      latestOf(['Firefox Setup 108.0.2.msi', 'Firefox Setup 152.0.4.exe']),
      []
    );
  });
  it('预发布点分：beta.2 > beta.1', () => {
    assert.deepStrictEqual(latestOf(['app-1.2.3-beta.1.exe', 'app-1.2.3-beta.2.exe']), ['app-1.2.3-beta.2.exe']);
  });
  it('预发布连字符：rc-2 > rc-1', () => {
    assert.deepStrictEqual(latestOf(['app-1.2.3-rc-1.exe', 'app-1.2.3-rc-2.exe']), ['app-1.2.3-rc-2.exe']);
  });
  it('预发布无分隔：rc2 > rc1', () => {
    assert.deepStrictEqual(latestOf(['app-1.2.3-rc1.exe', 'app-1.2.3-rc2.exe']), ['app-1.2.3-rc2.exe']);
  });
  it('chrome 安装包混合命名：归并后取版本最高 109', () => {
    const files = [
      '101.0.4951.54_chrome64_stable_windows_installer.exe',
      '107.0.5304.122_chrome64_stable_windows_installer.exe',
      '108.0.5359.48_chrome64_beta_windows_installer.exe',
      '109.0.5414.120_chrome64_installer.exe'
    ];
    assert.deepStrictEqual(latestOf(files), ['109.0.5414.120_chrome64_installer.exe']);
  });
});

describe('真实 file 对象输入', () => {
  it('应忽略目录项、返回对象输入下的 path', () => {
    const files = [
      { path: '/a/app-1.0.exe', name: 'app-1.0.exe', type: 'file', modifiedTime: '2026-01-01T00:00:00Z' },
      { path: '/a/app-2.0.exe', name: 'app-2.0.exe', type: 'file', modifiedTime: '2026-01-02T00:00:00Z' },
      { path: '/a/sub', name: 'sub', type: 'dir', modifiedTime: '2026-01-03T00:00:00Z' }
    ];
    const r = analyzeLatestVersions(files, {});
    assert.deepStrictEqual(Array.from(r.latestPaths), ['/a/app-2.0.exe']);
  });

  it('不同目录的同名家族互不影响（path 唯一标识）', () => {
    const files = [
      { path: '/a/app-1.0.exe', name: 'app-1.0.exe', type: 'file', modifiedTime: '2026-01-01T00:00:00Z' },
      { path: '/b/app-2.0.exe', name: 'app-2.0.exe', type: 'file', modifiedTime: '2026-01-02T00:00:00Z' }
    ];
    // 跨目录比较：视为同家族 2 个文件 → 高亮版本高的 app-2.0.exe
    assert.deepStrictEqual(Array.from(analyzeLatestVersions(files, {}).latestPaths), ['/b/app-2.0.exe']);
  });

  it('汉字文件名保留签名：两个无关中文文件不误归同一家族', () => {
    // 若汉字被当分隔符丢弃，28所.zip 与 智华.zip 会得到空签名 → 被当作同族，
    // 28所.zip 因"有同类 + 数字28"被误标。汉字参与签名后各自孤立 → 都不标。
    assert.deepStrictEqual(
      latestOf(['28所.zip', '智华.zip', '智华关键词.txt']),
      []
    );
  });

  it('汉字与数字组合不算数字版本号', () => {
    assert.deepStrictEqual(latestOf(['28所.zip']), []);
  });
});