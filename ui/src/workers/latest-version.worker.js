// 最新版本文件识别算法的模块型 Web Worker。
// 接收主线程 postMessage({ id, files, config })；files 为精简后的文件描述数组（path/name/type/modifiedTime），
// 交由算法在子线程内完成分组比版本，避免大列表阻塞主线程。结果回传 { id, latestPaths } 或 { id, error }。
import latestVersionModule from '@/assets/js/filelist-latest-version-algm.js'

const { analyzeLatestVersions } = latestVersionModule

self.onmessage = (e) => {
  const { id, files, config } = e.data
  try {
    const result = analyzeLatestVersions(files || [], config || {})
    self.postMessage({ id, latestPaths: Array.from(result.latestPaths) })
  } catch (error) {
    self.postMessage({ id, error: error && error.message ? error.message : String(error) })
  }
}