// ============================================
// 下载/预览跳转工具（一次性签名票据方案）
//
// 背景：登录凭证统一由后端 HttpOnly Cookie（fuzhan_token）承载，同源请求自动携带；
// 但浏览器原生 <a>/<img>/<video>、新窗口 window.open、以及迅雷等外部下载工具发起的
// 请求虽会带上 Cookie（SameSite=Lax 顶级导航），仍存在 URL 直连场景与登录态耦合的
// 复杂度，因此下载采用「预签名短时效 URL」标准做法（AWS S3/OSS 同款）：
//
//   认证（谁是你）  → Cookie 放行换票接口（或 Authorization 请求头）
//   授权（能下载什么）→ 换票接口签发短时效签名票据 t=（按时限生效，可重复读取）
//
// 换票接口：POST /api/v1/web/download/ticket，body { path, mode }，返回
// { data: { url, ticket } }，其中 url 为相对路径（已附 t=），可直接交给
// window.open / location.href 使用。
// ============================================
import { request } from '@/api'
import { ElMessage } from 'element-plus/es/components/message/index'
import { formatErrorMessage } from './error.js'

// 路径分类：用票据访问的下载 URL 前缀
const DOWNLOAD_PREFIX = '/download/'
const WEB_DOWNLOAD_PREFIX = '/api/v1/web/download/'
const ADMIN_DOWNLOAD_PREFIX = '/api/v1/web/admin/download/'
// 私有文件分享链接（强制登录开启时同样先登录，登录后直接整页跳转下载）
const SHARE_PREFIX = '/share/'

// 渠道枚举：download=下载（ForceAttachment）/ preview=内联预览（ForceInline）
export const TicketMode = {
  DOWNLOAD: 'download',
  PREVIEW: 'preview'
}

/**
 * 换取一次性下载/预览票据并打开（新标签）。
 * @param {string} path 目标相对路径，支持两种形式：
 *   - '/download/<分段编码后的路径>'   （普通/首页/热门/最近等下载）
 *   - '/api/v1/web/admin/download/<…>'（管理端下载）
 * @param {object} [opts]
 * @param {string} [opts.mode] TicketMode.DOWNLOAD | TicketMode.PREVIEW
 * @param {object} [opts.openOptions] { target, features } 透传给 openDownloadUrl
 * @returns {Promise<boolean>} 是否成功打开
 */
export async function downloadWithTicket(path, opts = {}) {
  const mode = opts.mode || TicketMode.DOWNLOAD
  const ticketPath = normalizeTicketRequestPath(path)
  if (!ticketPath) {
    ElMessage.error('无效的下载路径')
    return false
  }
  try {
    const res = await request.post('/download/ticket', { path: ticketPath, mode }, { skipSuccessToast: true })
    const url = res?.data?.url
    if (!url) {
      ElMessage.error('换取下载票据失败')
      return false
    }
    openDownloadUrl(url, opts.openOptions)
    return true
  } catch (err) {
    ElMessage.error(formatErrorMessage(err, '获取下载地址失败，请稍后重试'))
    return false
  }
}

/**
 * 校验并打开一个已附票据（t=）的相对/绝对下载地址。
 * 需在新标签/原生标签中访问（浏览器 + 外部下载工具均适用）。
 */
export function openDownloadUrl(url, openOptions = {}) {
  if (!url) return
  if (!isTicketUrl(url)) {
    ElMessage.error('下载地址无效，请刷新后重试')
    return
  }
  const { target = '_blank', features } = openOptions
  if (features) {
    window.open(url, target, features)
  } else {
    window.open(url, target)
  }
}

/**
 * 整页跳到带票据的下载地址（用于登录后的回跳：hash 路由会吞掉非 hash 路径，
 * 必须用整页跳转，不能用 router.replace）。
 */
export function redirectToDownload(url) {
  if (!url) return
  if (!isTicketUrl(url)) {
    ElMessage.error('下载地址无效，请返回后重试')
    return
  }
  window.location.href = url
}

// 换票接口只接受 /download、/api/v1/web/download 或 /admin/download 开头的路径，其它一律拒绝
function normalizeTicketRequestPath(path) {
  if (typeof path !== 'string') return ''
  const p = path.trim()
  if (p.startsWith(ADMIN_DOWNLOAD_PREFIX)) return p
  if (p.startsWith(WEB_DOWNLOAD_PREFIX)) return p
  if (p.startsWith(DOWNLOAD_PREFIX)) return p
  return ''
}

// 票据 URL 必须携带 t= 参数（防止误用普通无签名地址）
function isTicketUrl(url) {
  if (typeof url !== 'string' || !url) return false
  const queryIndex = url.indexOf('?')
  if (queryIndex < 0) return false
  const query = url.slice(queryIndex + 1)
  return new URLSearchParams(query).has('t')
}

// 下载类 redirect 前缀（登录后被 302 到登录页带回的地址）
const isDownloadRedirectPath = (p) =>
  p.startsWith(DOWNLOAD_PREFIX) || p.startsWith(WEB_DOWNLOAD_PREFIX) || p.startsWith(ADMIN_DOWNLOAD_PREFIX)

// redirect 是否为下载类路径（由后端 302 登录页携带，登录后应执行下载而非 SPA 跳转）
export function isDownloadRedirect(redirect) {
  if (typeof redirect !== 'string' || !redirect) return false
  const qi = redirect.indexOf('?')
  return isDownloadRedirectPath(qi >= 0 ? redirect.slice(0, qi) : redirect)
}

/**
 * 登录成功后的下载回跳：对下载类 redirect 换短时效票据后整页跳转执行下载。
 * hash 路由会吞掉非 hash 路径（catch-all → /files），因此必须整页跳转而非 router.replace。
 * 非下载类 redirect 返回 false，由调用方按 SPA 内部路径处理。
 * @returns {Promise<boolean>} 是否已发起整页下载
 */
export async function executeLoginDownload(redirect) {
  if (typeof redirect !== 'string' || !redirect) return false
  const qi = redirect.indexOf('?')
  const path = qi >= 0 ? redirect.slice(0, qi) : redirect

  // 私有文件分享链接：登录后直接整页跳转即可（无需换票）
  if (path.startsWith(SHARE_PREFIX)) {
    window.location.href = redirect
    return true
  }

  if (!isDownloadRedirect(redirect)) return false
  try {
    const res = await request.post('/download/ticket', { path, mode: TicketMode.DOWNLOAD }, { skipSuccessToast: true })
    const url = res?.data?.url
    if (!url || !isTicketUrl(url)) return false
    window.location.href = url
    return true
  } catch (err) {
    ElMessage.error(formatErrorMessage(err, '获取下载地址失败，请稍后重试'))
    return false
  }
}

export default {
  TicketMode,
  downloadWithTicket,
  openDownloadUrl,
  redirectToDownload,
  isDownloadRedirect,
  executeLoginDownload
}
