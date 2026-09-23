/**
# Node.js >= 18 原生支持，无需安装任何依赖
node --test filelist-latest-version-algm.test.js
node --test --test-reporter=spec filelist-latest-version-algm.test.js

# 如需 watch 模式（文件变更自动重跑）
node --test --watch filelist-latest-version-algm.test.js
*/
import { describe, it } from 'node:test';
import assert from 'node:assert/strict';
import latestVersionModule from './filelist-latest-version-algm.js';

const { analyzeLatestVersions, ParseCache, parseFile } = latestVersionModule;

// ============================================================================
// 辅助函数
// ============================================================================

/**
 * 运行分析并返回排序后的文件名数组（去除路径前缀），便于断言
 */
function getLatestFilenames(files, config = {}) {
  const result = analyzeLatestVersions(files, config);
  return Array.from(result.latestPaths)
    .map((p) => p.split('/').pop())
    .sort();
}

// ============================================================================
// 测试套件
// ============================================================================

describe('analyzeLatestVersions', () => {
  // ---------- 基础版本识别 ----------
  describe('基础语义化版本', () => {
    it('应选出最高 semver 版本', () => {
      const files = ['app-1.0.0.exe', 'app-1.1.0.exe', 'app-2.0.0.exe'];
      assert.deepStrictEqual(getLatestFilenames(files), ['app-2.0.0.exe']);
    });

    it('应正确处理多位数版本号', () => {
      const files = ['app-1.9.0.exe', 'app-1.10.0.exe', 'app-1.2.0.exe'];
      assert.deepStrictEqual(getLatestFilenames(files), ['app-1.10.0.exe']);
    });

    it('应在同版本时选无预发布的稳定版', () => {
      const files = ['app-1.0.0-beta.exe', 'app-1.0.0-rc.exe', 'app-1.0.0.exe'];
      assert.deepStrictEqual(getLatestFilenames(files), ['app-1.0.0.exe']);
    });

    it('应在同为预发布时选更高序号', () => {
      const files = ['app-1.0.0-beta.1.exe', 'app-1.0.0-beta.3.exe', 'app-1.0.0-beta.2.exe'];
      assert.deepStrictEqual(getLatestFilenames(files), ['app-1.0.0-beta.3.exe']);
    });
  });

  // ---------- 日期与括号版本 ----------
  describe('非标准版本格式', () => {
    it('应正确比较日期版本', () => {
      const files = ['report-20231201.pdf', 'report-20240315.pdf', 'report-20240101.pdf'];
      assert.deepStrictEqual(getLatestFilenames(files), ['report-20240315.pdf']);
    });

    it('应拒绝非法日期并回退到 NUM 比较', () => {
      // 20240230 不是有效日期 → 作为 NUM；20240228 是有效 DATE
      // DATE 优先级 > NUM，所以 20240228 胜出
      const files = ['log-20240230.txt', 'log-20240228.txt'];
      assert.deepStrictEqual(getLatestFilenames(files), ['log-20240228.txt']);
    });

    it('应正确处理闰年日期', () => {
      const files = ['report-20240229.pdf', 'report-20230229.pdf', 'report-20240301.pdf'];
      // 20240229 有效(闰年), 20230229 无效, 20240301 > 20240229
      assert.deepStrictEqual(getLatestFilenames(files), ['report-20240301.pdf']);
    });

    it('DATE 优先级应高于 PAREN', () => {
      const files = ['report(1)-20240101.pdf', 'report(5)-20230101.pdf'];
      assert.deepStrictEqual(getLatestFilenames(files), ['report(1)-20240101.pdf']);
    });

    it('应正确比较括号序号', () => {
      const files = ['file(1).txt', 'file(3).txt', 'file(2).txt'];
      assert.deepStrictEqual(getLatestFilenames(files), ['file(3).txt']);
    });
  });

  // ---------- 分组模式 ----------
  describe('Channel 分组模式', () => {
    it('merge 模式应将所有 channel 合并取最新', () => {
      const files = ['chrome-stable-1.2.3.exe', 'chrome-beta-1.3.0.exe', 'chrome-stable-1.2.4.exe'];
      assert.deepStrictEqual(
        getLatestFilenames(files, { channelGrouping: 'merge' }),
        ['chrome-beta-1.3.0.exe']
      );
    });

    it('rank 模式应优先选 stable channel 的最新版本', () => {
      const files = ['chrome-stable-1.2.3.exe', 'chrome-beta-1.3.0.exe', 'chrome-stable-1.2.4.exe'];
      assert.deepStrictEqual(
        getLatestFilenames(files, { channelGrouping: 'rank' }),
        ['chrome-stable-1.2.4.exe']
      );
    });

    it('separate 模式应按 channel 独立分组', () => {
      const files = [
        'chrome_stable_1.0.0.exe',
        'chrome_stable_1.1.0.exe',
        'chrome_beta_1.3.0.exe',
      ];
      assert.deepStrictEqual(
        getLatestFilenames(files, { channelGrouping: 'separate', includeLone: true }),
        ['chrome_beta_1.3.0.exe', 'chrome_stable_1.1.0.exe']
      );
    });

    it('separate 模式下孤例默认被过滤', () => {
      const files = [
        'chrome_stable_1.0.0.exe',
        'chrome_stable_1.1.0.exe',
        'chrome_beta_1.3.0.exe',
      ];
      assert.deepStrictEqual(
        getLatestFilenames(files, { channelGrouping: 'separate' }),
        ['chrome_stable_1.1.0.exe']
      );
    });
  });

  // ---------- Platform / Arch ----------
  describe('Platform 与 Arch 分组', () => {
    it('rank 模式应优先选无 platform 标记的文件', () => {
      const files = ['app-1.0.0-win.exe', 'app-1.0.0-mac.dmg', 'app-1.0.0.exe'];
      assert.deepStrictEqual(
        getLatestFilenames(files, { platformGrouping: 'rank' }),
        ['app-1.0.0.exe']
      );
    });

    it('rank 模式应优先选无 arch 标记的文件', () => {
      const files = ['app-1.0.0-x64.exe', 'app-1.0.0-arm64.exe', 'app-1.0.0.exe'];
      assert.deepStrictEqual(
        getLatestFilenames(files, { archGrouping: 'rank' }),
        ['app-1.0.0.exe']
      );
    });
  });

  // ---------- 扩展名与边界情况 ----------
  describe('扩展名与边界情况', () => {
    it('应正确处理双扩展名', () => {
      const files = ['pkg-1.0.tar.gz', 'pkg-2.0.tar.gz'];
      assert.deepStrictEqual(getLatestFilenames(files), ['pkg-2.0.tar.gz']);
    });

    it('不同扩展名应分为不同组（每组内部仍按同类判定）', () => {
      const files = ['app-1.0.exe', 'app-2.0.zip', 'app-1.5.exe'];
      // exe 组有 2 个同类 → 标 1.5；zip 组只有 1 个（无同类）→ 不标
      assert.deepStrictEqual(getLatestFilenames(files), ['app-1.5.exe']);
    });

    it('includeLone=true 时应保留孤例', () => {
      const files = ['unique-app-3.0.exe'];
      assert.deepStrictEqual(
        getLatestFilenames(files, { includeLone: true }),
        ['unique-app-3.0.exe']
      );
    });

    it('includeLone=false 时应过滤孤例', () => {
      const files = ['unique-app-3.0.exe'];
      assert.deepStrictEqual(getLatestFilenames(files, { includeLone: false }), []);
    });

    it('空输入应返回空结果', () => {
      const result = analyzeLatestVersions([], {});
      assert.strictEqual(result.latestPaths.size, 0);
      assert.strictEqual(result.confidence, 0);
    });

    it('应正确处理带目录的路径', () => {
      const files = ['/usr/bin/app-1.0', '/usr/bin/app-2.0', '/opt/app-1.5'];
      const result = analyzeLatestVersions(files, {});
      assert.deepStrictEqual(Array.from(result.latestPaths), ['/usr/bin/app-2.0']);
    });
  });

  // ---------- Signature N-token 跳过回归 ----------
  describe('Signature N-token 跳过 (FIX #1 回归)', () => {
    it('merge 模式下 beta.N 的 N 应被跳过，正确分组', () => {
      const files = ['app-1.0-beta.1.exe', 'app-1.0-beta.2.exe', 'app-1.0-beta.10.exe'];
      assert.deepStrictEqual(
        getLatestFilenames(files, { channelGrouping: 'merge' }),
        ['app-1.0-beta.10.exe']
      );
    });

    it('rank 模式下 rc.N 的 N 应被跳过', () => {
      const files = ['tool-3.0-rc.1.exe', 'tool-3.0-rc.2.exe', 'tool-3.0-rc.11.exe'];
      assert.deepStrictEqual(
        getLatestFilenames(files, { channelGrouping: 'rank' }),
        ['tool-3.0-rc.11.exe']
      );
    });
  });

  // ---------- 多组同时存在 ----------
  describe('多组文件', () => {
    it('应分别为每组选出最新版', () => {
      const files = ['editor-1.0.exe', 'editor-2.0.exe', 'viewer-1.0.exe', 'viewer-1.5.exe'];
      assert.deepStrictEqual(getLatestFilenames(files), ['editor-2.0.exe', 'viewer-1.5.exe']);
    });
  });
});

// ============================================================================
// ParseCache 测试
// ============================================================================

describe('ParseCache', () => {
  it('应正确存取缓存', () => {
    const cache = new ParseCache(10);
    cache.set('key1', { version: '1.0' });
    assert.deepStrictEqual(cache.get('key1'), { version: '1.0' });
  });

  it('未命中应返回 undefined', () => {
    const cache = new ParseCache(10);
    assert.strictEqual(cache.get('missing'), undefined);
  });

  it('超出 maxSize 时应淘汰最旧条目 (LRU)', () => {
    const cache = new ParseCache(3);
    cache.set('a', 1);
    cache.set('b', 2);
    cache.set('c', 3);
    cache.set('d', 4); // 触发淘汰 'a'
    assert.strictEqual(cache.get('a'), undefined);
    assert.strictEqual(cache.get('b'), 2);
    assert.strictEqual(cache.get('d'), 4);
  });

  it('get 操作应刷新 LRU 顺序', () => {
    const cache = new ParseCache(3);
    cache.set('a', 1);
    cache.set('b', 2);
    cache.set('c', 3);
    cache.get('a'); // 刷新 'a' 为最近使用
    cache.set('d', 4); // 应淘汰 'b' 而非 'a'
    assert.strictEqual(cache.get('a'), 1);
    assert.strictEqual(cache.get('b'), undefined);
  });

  it('集成到 analyzeLatestVersions 时应正常工作', () => {
    const cache = new ParseCache(100);
    const files = ['cached-1.0.bin', 'cached-2.0.bin'];
    const result = analyzeLatestVersions(files, { cache });
    assert.deepStrictEqual(getLatestFilenames(files, { cache }), ['cached-2.0.bin']);
    // 验证缓存已被填充
    assert.notStrictEqual(cache.get('cached-1.0.bin'), undefined);
    assert.notStrictEqual(cache.get('cached-2.0.bin'), undefined);
  });
});

// ============================================================================
// parseFile 单元测试
// ============================================================================

describe('parseFile', () => {
  it('应正确解析标准版本文件', () => {
    const pf = parseFile('app-1.2.3-beta.1.exe', { channelGrouping: 'merge' });
    assert.strictEqual(pf.version, '1.2.3');
    assert.ok(pf.pre?.includes('beta'));
    assert.strictEqual(pf.ext, 'exe');
  });

  it('应正确提取双扩展名', () => {
    const pf = parseFile('archive-1.0.tar.gz', {});
    assert.strictEqual(pf.ext, 'tar.gz');
    assert.strictEqual(pf.base, 'archive-1.0');
  });

  it('无扩展名文件 ext 应为空字符串', () => {
    const pf = parseFile('README', {});
    assert.strictEqual(pf.ext, '');
    assert.strictEqual(pf.base, 'README');
  });
});