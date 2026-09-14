import { h } from 'vue'
import { ElButton, ElCheckbox } from 'element-plus'
import { TimeUtils, NumberUtils } from '@/utils'
import { isPreviewable } from '@/config/preview'

// 是否为目录（统一兼容 dir/directory 两种类型标识）
export const isDir = (row) => row.type === 'dir' || row.type === 'directory'

// 获取文件类型图标类名
export const getFileIconClass = (row) => {
  if (isDir(row)) return 'icon-filetype-folder'
  const ext = row.name?.split('.').pop()?.toLowerCase() || ''
  return `icon-filetype-${ext}`
}

// 格式化文件大小
export const formatSize = (bytes) => bytes === 0 ? '-' : NumberUtils.formatFileSize(bytes)

// 判断文件是否可预览
export const canPreview = (row) => {
  if (isDir(row)) return false
  return isPreviewable(row.name)
}

// 对路径的每段分别编码，避免斜杠被编码
export const encodePath = (path) => {
  return path.split('/').filter(Boolean).map(p => encodeURIComponent(p)).join('/')
}

/**
 * 构建文件列表表格列定义。
 * 渲染器依赖一组由调用方注入的回调/响应式值，避免在此文件引入对视图与 store 的直接依赖。
 */
export const buildFileColumns = ({
  isAtRoot, checkedRowKeys, isAllSelected, isIndeterminate,
  toggleSelectAll, handleSingleCheck, currentPath, router,
  highlightKeyword, adminDownloadHref, previewFile,
  isSearching, searchCompleted, navigateToDir,
  openMoveModal, handleDelete, openNotesEditor
}) => {
  return [
    {
      key: 'selection',
      width: 40,
      headerCellRenderer: () => h(ElCheckbox, {
        modelValue: isAllSelected.value,
        indeterminate: isIndeterminate.value,
        disabled: isAtRoot.value,
        onChange: (val) => toggleSelectAll(val)
      }),
      cellRenderer: ({ rowData: row }) => h(ElCheckbox, {
        modelValue: checkedRowKeys.value.includes(row.path),
        disabled: isAtRoot.value,
        onChange: (val) => handleSingleCheck(row, val)
      })
    },
    {
      title: '文件名',
      key: 'name',
      minWidth: 240,
      flexGrow: 1,
      cellRenderer: ({ rowData: row }) => {
        const iconClass = `icon-filetype ${getFileIconClass(row)}`
        // 导航到子目录：row.path 已是完整路径 (rootName/subPath)
        const dirPath = row.path
        const fileHref = adminDownloadHref(row.path)
        const previewable = canPreview(row)

        // 直接浏览模式（与 HomeView 一致）
        const renderNormalMode = () => [
          h('div', { class: 'file-icon-wrapper' }, [
            h('span', { class: iconClass }),
            previewable && !isDir(row) ? h('span', { class: 'preview-icon' }) : null
          ]),
          isDir(row)
            ? h('a', {
              class: 'file-link dir-link',
              onClick: (e) => {
                e.preventDefault()
                router.push('/admin/files/' + encodePath(dirPath))
              }
            }, highlightKeyword(row.name))
            : h('a', {
              href: fileHref,
              class: 'file-link',
              onClick: (e) => {
                if (previewable) {
                  e.preventDefault()
                  previewFile(row)
                }
              }
            }, highlightKeyword(row.name))
        ]

        // 搜索结果模式（显示完整路径，与 HomeView 一致）
        const renderSearchMode = () => {
          const fullPath = row.path || ''
          const segments = fullPath.split('/').filter(Boolean)
          const fileName = segments[segments.length - 1] || ''
          const pathSegments = segments.slice(0, -1)

          return [
            h('div', { class: 'file-icon-wrapper' }, [
              h('span', { class: iconClass }),
              previewable && !isDir(row) ? h('span', { class: 'preview-icon' }) : null
            ]),
            h('span', { class: 'file-path-content' }, [
              ...pathSegments.map((seg, idx) => {
                const segPath = '/' + pathSegments.slice(0, idx + 1).join('/')
                return [
                  h('a', {
                    class: 'path-segment',
                    onClick: (e) => {
                      e.preventDefault()
                      e.stopPropagation()
                      navigateToDir(segPath)
                    }
                  }, highlightKeyword(seg)),
                  '/'
                ]
              }).flat(),
              isDir(row)
                ? h('a', {
                  class: 'file-link dir-link',
                  onClick: (e) => {
                    e.preventDefault()
                    navigateToDir(dirPath)
                  }
                }, highlightKeyword(fileName))
                : h('a', {
                  href: fileHref,
                  class: 'file-link',
                  onClick: (e) => {
                    if (previewable) {
                      e.preventDefault()
                      previewFile(row)
                    }
                  }
                }, highlightKeyword(fileName))
            ])
          ]
        }

        return h('div', {
          class: 'file-name-cell',
          title: row.name
        }, isSearching.value || searchCompleted.value ? renderSearchMode() : renderNormalMode())
      }
    },
    { title: '大小', key: 'size', width: 150, cellRenderer: ({ rowData: row }) => formatSize(row.size) },
    {
      title: '修改时间',
      key: 'modifiedTime',
      width: 200,
      cellRenderer: ({ rowData: row }) => {
        const text = TimeUtils.formatDateTime(row.modifiedTime)
        if (TimeUtils.isRecent24h(row.modifiedTime)) {
          return h('span', { style: 'color: #18a058' }, text)
        }
        return text
      }
    },
    {
      title: '备注',
      key: 'notes',
      width: 150,
      cellRenderer: ({ rowData: row }) => h(ElButton, {
        size: 'small', link: true,
        class: ['notes-link', { 'is-empty': !row.notes }],
        title: row.notes || '',
        onClick: () => openNotesEditor(row)
      }, () => h('span', { class: 'notes-text' }, row.notes || '添加备注'))
    },
    {
      title: '操作',
      key: 'actions',
      width: 200,
      cellRenderer: ({ rowData: row }) => {
        // 根目录不显示操作按钮
        if (isAtRoot.value) {
          return null
        }
        return h('div', { class: 'action-buttons' }, [
          h(ElButton, { size: 'small', link: true, onClick: () => openMoveModal(row) }, () => '移动/重命名'),
          h(ElButton, { size: 'small', link: true, type: 'danger', onClick: () => handleDelete(row) }, () => '删除')
        ])
      }
    }
  ]
}