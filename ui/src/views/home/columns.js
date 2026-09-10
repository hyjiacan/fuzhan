import { h } from 'vue'
import { ElButton } from 'element-plus'
import { NumberUtils, TimeUtils, PathUtils } from '@/utils'
import { isPreviewable } from '@/config/preview'
import { highlightKeyword } from './highlight'

const isDir = (row) => row.type === 'dir' || row.type === 'directory'

const getFileIconClass = (row) => {
  if (isDir(row)) return 'icon-filetype-folder'
  const ext = row.name?.split('.').pop()?.toLowerCase() || ''
  return `icon-filetype-${ext}`
}

const canPreview = (row) => {
  if (isDir(row)) return false
  return isPreviewable(row.name)
}

const formatSize = (bytes) => bytes === 0 ? '-' : NumberUtils.formatFileSize(bytes)

// 构建文件列表 el-table-v2 列定义
export function createColumns(ctx) {
  const {
    latestVersionPaths,
    isSearching,
    searchCompleted,
    searchQuery,
    navigateToDir,
    previewFile,
    openNotesEditor,
    openDepTree,
    refreshAfterDownload
  } = ctx

  // 文件名单元格（复用搜索高亮逻辑）
  const renderFileNameCell = (row) => {
    const iconClass = `icon-filetype ${getFileIconClass(row)}`
    const isLatest = latestVersionPaths.value.has(row.path)
    const dirPath = row.path
    const downloadFullPath = row.path
    const fileHref = `/download/${PathUtils.encodeFilePath(downloadFullPath)}`
    const isPreview = canPreview(row)
    const fullPath = row.path || ''
    const segments = fullPath.split('/').filter(Boolean)
    const fileName = segments[segments.length - 1] || ''
    const pathSegments = segments.slice(0, -1)
    const highlight = (text) => highlightKeyword(text, searchQuery.value, isSearching.value || searchCompleted.value)

    // 直接浏览模式（不显示路径）
    const renderNormalMode = () => [
      h('div', { class: 'file-icon-wrapper' }, [
        h('span', { class: iconClass }),
        isPreview && !isDir(row) ? h('span', { class: 'preview-icon' }) : null
      ]),
      isDir(row)
        ? h('a', {
          class: 'file-link',
          onClick: (e) => {
            e.preventDefault()
            navigateToDir(dirPath)
          }
        }, highlight(fileName))
        : h('a', {
          href: fileHref,
          class: isLatest ? 'file-link latest-version' : 'file-link',
          onClick: (e) => {
            if (isPreview) {
              e.preventDefault()
              previewFile(row)
              return
            }
            // 非预览：允许默认下载行为，稍后刷新列表更新下载次数
            refreshAfterDownload()
          }
        }, highlight(fileName))
    ]

    // 搜索结果模式（显示完整路径）
    const renderSearchMode = () => [
      h('div', { class: 'file-icon-wrapper' }, [
        h('span', { class: iconClass }),
        isPreview && !isDir(row) ? h('span', { class: 'preview-icon' }) : null
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
            }, highlight(seg)),
            '/'
          ]
        }).flat(),
        isDir(row)
          ? h('a', {
            class: 'file-link',
            onClick: (e) => {
              e.preventDefault()
              navigateToDir(dirPath)
            }
          }, highlight(fileName))
          : h('a', {
            href: fileHref,
            class: 'file-link',
            onClick: (e) => {
              if (isPreview) {
                e.preventDefault()
                previewFile(row)
                return
              }
              refreshAfterDownload()
            }
          }, highlight(fileName))
      ])
    ]

    return h('div', {
      class: 'file-name-cell',
      title: fileName
    }, isSearching.value || searchCompleted.value ? renderSearchMode() : renderNormalMode())
  }

  return [
    {
      title: '文件名',
      key: 'name',
      minWidth: 240,
      flexGrow: 1,
      sortable: true,
      sortBy: 'name',
      cellRenderer: ({ rowData: row }) => renderFileNameCell(row)
    },
    {
      title: '大小', key: 'size', width: 150,
      cellRenderer: ({ rowData: row }) => formatSize(row.size)
    },
    {
      title: '修改时间', key: 'modifiedTime', width: 200, sortable: true, sortBy: 'modifiedTime',
      cellRenderer: ({ rowData: row }) => {
        const text = TimeUtils.formatDateTime(row.modifiedTime)
        if (TimeUtils.isRecent24h(row.modifiedTime)) {
          return h('span', { style: 'color: #18a058' }, text)
        }
        return text
      }
    },
    {
      title: '下载次数', key: 'downloadCount', width: 110, sortable: true, sortBy: 'downloadCount',
      cellRenderer: ({ rowData: row }) => isDir(row) ? '' : ((row.downloadCount || 0) > 0 ? row.downloadCount : '-')
    },
    {
      title: '备注', key: 'notes', width: 150,
      cellRenderer: ({ rowData: row }) => h(ElButton, {
        size: 'small', link: true,
        class: ['notes-link', { 'is-empty': !row.notes }],
        title: row.notes || '',
        onClick: () => openNotesEditor(row)
      }, () => h('span', { class: 'notes-text' }, row.notes || '添加备注'))
    },
    {
      title: '操作', key: 'actions', width: 150,
      cellRenderer: ({ rowData: row }) => {
        // 目录：不显示管理操作（公开文件的管理已迁移至管理员页面）
        if (isDir(row)) return null
        return h(ElButton, { size: 'small', link: true, onClick: () => openDepTree(row) }, () => '依赖')
      }
    }
  ]
}