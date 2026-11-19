import { ElMessageBox } from 'element-plus'
import { AuthApi } from '@/api'

// 管理员为当前账号绑定外部身份后，用户首次登录时提示一次。
// provider 为后端返回的认证源显示名称（如「企业 SSO」「LDAP 目录（dc.example.com）」）。
// 展示后无论确认还是关闭弹框都回写 ack，保证只提示一次；失败不影响登录流程。
export const promptIdentityNotice = (pending, provider) => {
  if (!pending) return
  const source = provider ? `「${provider}」` : '外部认证身份'
  ElMessageBox.alert(
    `管理员已将你的账号与${source}完成绑定，此后请使用该认证方式登录。如非本人操作，请立即联系管理员。`,
    '账号认证方式已变更',
    { type: 'warning', confirmButtonText: '我知道了' }
  )
    .catch(() => {})
    .finally(() => {
      AuthApi.ackIdentityNotice().catch(() => {})
    })
}

// 本地账号登录成功后发现目录中存在同名账号：提示风险，并提供自助绑定入口。
// 返回 Promise<boolean>：用户选择「立即绑定」时为 true，取消/关闭为 false。
// canSelfBind=false（如管理员账号，后端不允许自助绑定）时只提示联系管理员。
export const promptIdentityConflict = (pending, provider, options = {}) => {
  if (!pending) return Promise.resolve(false)
  const source = provider ? `「${provider}」` : '外部认证目录'
  if (options.canSelfBind === false) {
    ElMessageBox.alert(
      `检测到${source}中还存在同名账号，当前登录使用的仍是本地账号。请联系管理员确认并完成身份绑定，避免登录方式与数据不一致。`,
      '检测到同名账号',
      { type: 'warning', confirmButtonText: '我知道了' }
    ).catch(() => {})
    return Promise.resolve(false)
  }
  return ElMessageBox.confirm(
    `检测到${source}中还存在同名账号，当前登录使用的仍是本地账号。可直接在「个人中心 → 外部账号绑定」输入目录密码完成绑定（绑定后本地密码会被清除）；如非本人账号，请联系管理员处理。`,
    '检测到同名账号',
    { type: 'warning', confirmButtonText: '立即绑定', cancelButtonText: '稍后处理' }
  )
    .then(() => true)
    .catch(() => false)
}
