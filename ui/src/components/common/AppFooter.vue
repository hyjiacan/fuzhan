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
				<a href="https://gitee.com/hyjiacan/fuzhan" target="_blank">轻共享</a>
				<span class="divider">•</span>
				<a href="https://gitee.com/hyjiacan/fuzhan/issues/new" target="_blank">反馈建议</a>
				<span class="divider">•</span>
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

			<el-dialog v-model="showHelp" title="帮助" width="500px">
				<div class="help-content">
					<h4>基本操作</h4>
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
					<h4>更多帮助</h4>
					<p>
						<a href="https://gitee.com/hyjiacan/fuzhan" target="_blank">访问项目主页</a>
					</p>
				</div>
				<template #footer>
					<el-button type="primary" @click="showHelp = false">关闭</el-button>
				</template>
			</el-dialog>

			<el-dialog v-model="showAbout" title="关于" width="400px">
				<div class="about-content">
					<div class="about-logo">
						<el-icon size="48" color="#18a058">
							<svg viewBox="0 0 24 24" fill="currentColor">
								<path d="M19.35 10.04C18.67 6.59 15.64 4 12 4 9.11 4 6.6 5.64 5.35 8.04 2.34 8.36 0 10.91 0 14c0 3.31 2.69 6 6 6h13c2.76 0 5-2.24 5-5 0-2.64-2.05-4.78-4.65-4.96z"/>
							</svg>
						</el-icon>
					</div>
					<h3>轻共享</h3>
					<p class="version">版本 1.0.0</p>
					<p class="description">轻量级文件共享服务，支持文件上传、下载、预览和分享。</p>
					<div class="about-info">
						<div class="info-row">
							<span class="info-label">项目地址</span>
							<a href="https://gitee.com/hyjiacan/fuzhan" target="_blank">gitee.com/hyjiacan/fuzhan</a>
						</div>
						<div class="info-row">
							<span class="info-label">运行环境</span>
							<span class="info-value" id="runtime-info">{{ runtimeInfo }}</span>
						</div>
					</div>
				</div>
				<template #footer>
					<el-button type="primary" @click="showAbout = false">关闭</el-button>
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

const showHelp = ref(false)
const showAbout = ref(false)
const runtimeInfo = ref('')

const fetchRuntimeInfo = async () => {
	try {
		const res = await fetch('/api/v1/health')
		if (res.ok) {
			const data = await res.json()
			runtimeInfo.value = `${data.goVersion || ''}`
		}
	} catch {
		runtimeInfo.value = ''
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
		}

		.about-content {
			text-align: center;
			padding: 8px 0;

			.about-logo {
				margin-bottom: 12px;
			}

			h3 {
				margin: 0 0 4px;
				font-size: 20px;
				color: @text-color;
			}

			.version {
				margin: 0 0 12px;
				color: @text-color-secondary;
				font-size: @font-size-sm;
			}

			.description {
				margin: 0 0 16px;
				color: @text-color-secondary;
				font-size: @font-size-sm;
			}

			.about-info {
				text-align: left;
				border-top: 1px solid @border-color-light;
				padding-top: 12px;

				.info-row {
					display: flex;
					justify-content: space-between;
					align-items: center;
					padding: 4px 0;
					font-size: @font-size-sm;

					.info-label {
						color: @text-color-secondary;
					}

					.info-value {
						color: @text-color;
					}

					a {
						color: @primary-color;
					}
				}
			}
		}
	}
</style>
