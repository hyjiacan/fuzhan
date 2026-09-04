<template>
	<footer class="app-footer">
			<div class="footer-left">
				<a @click="showPreferences = true" class="link-button">偏好设置</a>
				<IndexStatusBar @triggerScan="handleTriggerScan" />
			</div>
			<div class="footer-right">
					<a @click="showHelp = true" class="link-button">帮助</a>
					<span class="divider">•</span>
					<a @click="showAbout = true" class="link-button">关于</a>
					<span class="divider">•</span>
					<a v-if="openApiEnabled" href="/scalar.html" target="_blank" title="OpenAPI 接口文档">API 文档</a>
					<span v-if="openApiEnabled" class="divider">•</span>
					<span>hyjiacan © 2025</span>
				</div>

			<el-dialog v-model="showPreferences" title="偏好设置" width="400px">
				<div class="preference-item">
					<span class="preference-label">页面宽度</span>
					<el-select v-model="pageWidth" @change="savePreferences">
						<el-option
							v-for="opt in widthOptions"
							:key="opt.value"
							:label="opt.label"
							:value="opt.value"
						/>
					</el-select>
				</div>
				<template #footer>
					<el-button type="primary" @click="showPreferences = false">关闭</el-button>
				</template>
			</el-dialog>

			<el-dialog v-model="showHelp" title="帮助" width="720px">
					<div class="help-content">
						<el-tabs v-model="activeHelpTab">
							<el-tab-pane label="基本操作" name="basic">
								<h4>文件操作</h4>
								<ul>
									<li>上传文件：点击"上传文件"按钮或拖拽文件到页面</li>
									<li>下载文件：点击文件后的"下载"按钮</li>
									<li>预览文件：点击文件名或"预览"按钮</li>
									<li>分享文件：右键点击文件选择"分享"</li>
								</ul>
								<h4>临时文件</h4>
								<ul>
									<li>无需登录即可上传和下载</li>
									<li>文件到期后自动删除</li>
									<li>每个 IP 有上传配额限制</li>
								</ul>
							</el-tab-pane>
							<el-tab-pane label="命令行工具 (CLI)" name="cli">
									<div class="tab-scroll">
										<p>在终端中直接搜索、浏览和下载文件。当前服务器地址：<code>{{ cliBase }}</code></p>
										<h4>直接访问（无需安装）</h4>
										<ul>
											<li>使用 <code>curl</code> / <code>wget</code> 直接访问服务器地址，会自动返回命令行帮助文本（而非网页）：</li>
										</ul>
										<pre><code>curl {{ cliBase }}</code></pre>
										<ul>
											<li>直接访问 CLI：<a :href="cliBase + '/cli'" target="_blank">{{ cliBase }}/cli</a></li>
											<li>下载独立的 fuzhan 脚本：<a :href="cliBase + '/cli/fuzhan.sh'" target="_blank">{{ cliBase }}/cli/fuzhan.sh</a></li>
										</ul>
										<h4>安装到 PATH</h4>
										<p class="cli-install">执行安装脚本，将 fuzhan 安装到用户 PATH 目录，之后可直接使用 fuzhan 命令：</p>
										<pre><code>bash &lt;(curl -s {{ cliBase }}/cli/install.sh)</code></pre>
										<p class="cli-install">或先下载再执行：</p>
										<pre><code>curl -o install.sh {{ cliBase }}/cli/install.sh &amp;&amp; bash install.sh</code></pre>
										<h4>命令</h4>
										<ul>
											<li><code>search, s &lt;关键词...&gt;</code> 搜索文件（空格分隔多个关键词，AND 逻辑；<code>.pdf</code> 指定扩展名）</li>
											<li><code>list, l [路径]</code> 浏览目录（省略路径时列出所有根目录）</li>
											<li><code>download, dl &lt;路径或哈希值&gt;</code> 下载文件到当前目录（自动识别路径或哈希）</li>
											<li><code>help, h</code> 查看完整帮助</li>
										</ul>
										<h4>选项</h4>
										<ul>
											<li><code>--url, -u</code> 显示完整下载链接（search/list 默认只显示文件路径）</li>
										</ul>
										<h4>示例</h4>
										<ul>
											<li><code>fuzhan search document</code> 搜索文件名包含 document 的文件</li>
											<li><code>fuzhan search .pdf</code> 搜索所有 PDF 文件</li>
											<li><code>fuzhan list</code> 列出所有根目录</li>
											<li><code>fuzhan list root/doc</code> 列出 root/doc 目录下的内容</li>
											<li><code>fuzhan download root/doc/report.pdf</code> 下载文件到当前目录</li>
											<li><code>fuzhan download A1B2C3D4</code> 按哈希值下载</li>
										</ul>
									</div>
								</el-tab-pane>
						</el-tabs>
					</div>
					<template #footer>
						<el-button type="primary" @click="showHelp = false">关闭</el-button>
					</template>
				</el-dialog>

			<el-dialog v-model="showAbout" title="关于" width="560px">
					<div class="about-content">
						<div class="about-logo">
							<img src="/assets/icons/logo.svg" class="about-logo-img" alt="logo" />
						</div>
						<h3 class="about-title">浮栈 (Fuzhan)</h3>
						<p class="about-tagline">一座文件客栈，纳四方文件，可暂歇，可长驻</p>
						<p class="description">基于 Go 的轻量级文件共享工具，无需复杂数据库或服务器配置，直接启动即可通过浏览器访问。</p>
						<div class="about-info">
							<div class="info-row">
								<span class="info-label">版本</span>
								<span class="info-value">{{ aboutInfo.version || '—' }}</span>
							</div>
							<div class="info-row">
								<span class="info-label">运行环境</span>
								<span class="info-value">{{ aboutInfo.goVersion || '—' }}</span>
							</div>
							<div class="info-row">
								<span class="info-label">技术栈</span>
								<span class="info-value">Go + Gin · SQLite · Vue 3</span>
							</div>
							<div class="info-row">
								<span class="info-label">服务状态</span>
								<span class="info-value about-status">
									<span class="status-dot" :class="statusClass"></span>{{ statusText }}
								</span>
							</div>
						</div>
						<div class="about-links">
							<div class="info-row">
								<span class="info-label">项目地址</span>
								<span class="info-value">
									<a href="https://gitee.com/hyjiacan/fuzhan" target="_blank">Gitee</a>
									<span class="link-sep">·</span>
									<a href="https://github.com/hyjiacan/fuzhan" target="_blank">GitHub</a>
								</span>
							</div>
							<div class="info-row">
								<span class="info-label">开源协议</span>
								<a href="https://www.gnu.org/licenses/gpl-3.0.html" target="_blank">GPLv3 许可证</a>
							</div>
							<div class="info-row">
								<span class="info-label">反馈建议</span>
								<a href="https://gitee.com/hyjiacan/fuzhan/issues/new" target="_blank">提交 Issue</a>
							</div>
						</div>
					</div>
					<template #footer>
						<el-button type="primary" plain @click="showAbout = false">关闭</el-button>
					</template>
				</el-dialog>
		</footer>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import store from '@/store'
import IndexStatusBar from './IndexStatusBar.vue'
import { IndexApi } from '@/api'

const openApiEnabled = computed(() => store.state.config.openApiEnabled)

const cliBase = computed(() => window.location.origin)

const showHelp = ref(false)
const activeHelpTab = ref('basic')
const showAbout = ref(false)
const aboutInfo = ref({})

const statusText = computed(() => {
  const s = aboutInfo.value.status
  if (s === 'healthy') return '运行正常'
  if (s === 'degraded') return '部分异常'
  if (s === 'unhealthy') return '异常'
  return '未知'
})

const statusClass = computed(() => {
  const s = aboutInfo.value.status
  if (s === 'healthy') return 'ok'
  if (s === 'degraded') return 'warn'
  if (s === 'unhealthy') return 'bad'
  return 'unknown'
})

const fetchRuntimeInfo = async () => {
	try {
		const res = await fetch('/api/v1/health')
		if (res.ok) {
			const body = await res.json()
			aboutInfo.value = body.data || {}
		}
	} catch {
		aboutInfo.value = {}
	}
}

const handleTriggerScan = async () => {
	try {
		await IndexApi.triggerFullScan()
		ElMessage.success('全量扫描已触发')
	} catch (e) {
		ElMessage.error('触发扫描失败: ' + (e.message || '未知错误'))
	}
}

const STORAGE_KEY = 'page-width-preference'

const showPreferences = ref(false)
const pageWidth = ref('80%')

const widthOptions = [
	{ label: '50%', value: '50%' },
	{ label: '60%', value: '60%' },
	{ label: '70%', value: '70%' },
	{ label: '80%', value: '80%' },
	{ label: '90%', value: '90%' },
	{ label: '100%', value: '100%' },
]

const loadPreferences = () => {
	const saved = localStorage.getItem(STORAGE_KEY)
	if (saved) {
		pageWidth.value = saved
	}
}

const savePreferences = (value) => {
	localStorage.setItem(STORAGE_KEY, value)
	document.body.style.setProperty('--app-width', value)
}

onMounted(() => {
		loadPreferences()
		fetchRuntimeInfo()
	})
</script>

<style lang="less">
@import '@/styles/variables.less';

.app-footer {
	height: 40px;
	padding: 6px 20px;
	background: transparent;
	display: flex;
	justify-content: space-between;
	align-items: center;
	border-top: 1px solid @border-color-light;
	flex-shrink: 0;

	.footer-left {
		display: flex;
		align-items: center;
		gap: 12px;

		.link-button {
			cursor: pointer;
			color: @text-color-secondary;
			font-size: @font-size-sm;
			transition: color 0.2s;

			&:hover {
				color: @primary-color;
			}
		}
	}

	.footer-right {
		color: @text-color-secondary;
		font-size: @font-size-sm;
		display: flex;
		align-items: center;
		gap: 12px;

		.link-button {
			cursor: pointer;
			transition: color 0.2s;

			&:hover {
				color: @primary-color;
			}
		}

		.divider {
			margin: 0 8px;
		}
	}

	.preference-item {
			display: flex;
			align-items: center;
			justify-content: space-between;
			padding: 12px 0;

			.preference-label {
				font-weight: 500;
			}

			.el-select {
				width: 120px;
			}
		}

		.help-content {
			h4 {
				margin: 16px 0 8px;
				color: @text-color;
				font-size: @font-size-base;
			}
			h4:first-child {
				margin-top: 0;
			}
			ul {
				padding-left: 20px;
				margin: 0;
				li {
					margin: 4px 0;
					color: @text-color-secondary;
					font-size: @font-size-sm;
				}
			}
			p {
				margin: 8px 0 0;
				a {
					color: @primary-color;
				}
			}
			code {
				padding: 1px 5px;
				margin: 0 2px;
				border-radius: 3px;
				font-family: 'SFMono-Regular', Consolas, 'Liberation Mono', Menlo, monospace;
				font-size: 12px;
				background: @bg-color-secondary;
				color: @text-color;
			}
			.cli-install {
				font-size: @font-size-sm;
				margin-top: 10px;
			}
			pre {
				margin: 6px 0;
				padding: 8px 12px;
				overflow-x: auto;
				border-radius: 4px;
				background: @bg-color-secondary;
				border: 1px solid @border-color-light;
				code {
					margin: 0;
					padding: 0;
					background: transparent;
					font-size: 12px;
					line-height: 1.6;
					white-space: pre;
				}
			}
			.tab-scroll {
				max-height: 42vh;
				overflow-y: auto;
				padding-right: 4px;
			}
			.el-tabs__content {
				padding-top: 8px;
			}
		}

		.about-content {
			text-align: center;
			padding: 8px 0;

			.about-logo {
				margin-bottom: 12px;
			}

			.about-logo-img {
				width: 56px;
				height: 56px;
			}

			.about-title {
				margin: 0 0 2px;
				font-size: 20px;
				color: @text-color;
			}

			.about-tagline {
				margin: 0 0 10px;
				color: @text-color-secondary;
				font-size: @font-size-sm;
				font-style: italic;
			}

			.description {
				margin: 0 0 16px;
				color: @text-color-secondary;
				font-size: @font-size-sm;
				line-height: 1.7;
			}

			.about-info,
			.about-links {
				text-align: left;
				border-top: 1px solid @border-color-light;
				padding-top: 12px;
				margin-top: 14px;
			}

			.info-row {
				display: flex;
				justify-content: space-between;
				align-items: center;
				padding: 4px 0;
				font-size: @font-size-sm;

				.info-label {
					color: @text-color-secondary;
					flex-shrink: 0;
					margin-right: 12px;
				}

				.info-value {
					color: @text-color;
					display: flex;
					align-items: center;
					gap: 6px;
					text-align: right;
				}

				a {
					color: @primary-color;
				}

				.link-sep {
					color: @text-color-placeholder;
				}
			}

			.about-status {
					.status-dot {
						width: 8px;
						height: 8px;
						border-radius: 50%;
						display: inline-block;

						&.ok {
							background: @success-color;
						}
						&.warn {
							background: @warning-color;
						}
						&.bad {
							background: @error-color;
						}
						&.unknown {
							background: @text-color-placeholder;
						}
					}
				}
			}
		}
</style>
