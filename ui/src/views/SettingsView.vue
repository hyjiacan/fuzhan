<template>
  <div class="settings-view">
    <div class="header-section">
      <h2>系统设置</h2>
      <p class="description">管理系统配置和参数调整</p>
    </div>

    <el-tabs v-model="activeTab">
      <!-- 基本信息 -->
      <el-tab-pane name="basic" label="基本信息">
        <el-row :gutter="16">
          <el-col :span="12" :sm="12" :xs="24" style="margin-bottom: 16px;">
            <el-card>
              <template #header><span>应用信息</span></template>
              <el-form label-width="120px">
                <el-form-item label="应用名称">
                  <el-input v-model="settings.appName" :maxlength="64" placeholder="请输入应用名称" />
                  <div class="field-hint">显示在页面标题和界面顶部的应用名称</div>
                </el-form-item>
              </el-form>
            </el-card>
          </el-col>

          <el-col :span="12" :sm="12" :xs="24" style="margin-bottom: 16px;">
            <el-card>
              <template #header><span>共享目录配置</span></template>
              <div v-for="(dir, index) in settings.rootDirs" :key="index" style="margin-bottom: 12px; padding: 12px; background: #f8f9fa; border-radius: 8px;">
                <el-form label-width="100px">
                  <el-form-item label="目录路径">
                    <el-input v-model="dir.path" :maxlength="1024" placeholder="目录路径" />
                    <div class="field-hint">共享目录的绝对路径或相对路径</div>
                  </el-form-item>
                  <el-form-item label="显示名称">
                    <el-input v-model="dir.name" :maxlength="255" placeholder="显示名称" />
                    <div class="field-hint">在界面上显示的目录名称</div>
                  </el-form-item>
                </el-form>
                <el-button type="danger" size="small" @click="removeDir(index)">删除</el-button>
              </div>
              <el-button plain block @click="addDir">添加共享目录</el-button>
            </el-card>
          </el-col>

          <el-col :span="12" :sm="12" :xs="24" style="margin-bottom: 16px;">
            <el-card>
              <template #header><span>私有文件配置</span></template>
              <el-form label-width="120px">
                <el-form-item label="启用私有文件">
                  <el-switch v-model="settings.privateFiles.enabled" />
                  <div class="field-hint">开启后用户需登录才能上传文件</div>
                </el-form-item>
                <el-form-item label="存储路径">
                  <el-input v-model="settings.privateFiles.path" :maxlength="1024" placeholder="私有文件存储路径" />
                  <div class="field-hint">私有文件的存储目录</div>
                </el-form-item>
                <el-form-item label="全局配额">
                  <div style="display: flex; align-items: center; gap: 12px;">
                    <el-input v-model="privateQuotaGlobalDisplay" :maxlength="32" placeholder="如 500m, 10g, 1t" style="width: 200px;" />
                    <span style="color: #999;">{{ formatSize(settings.privateQuotaGlobal) }}</span>
                  </div>
                  <div class="field-hint">所有私有文件的总存储上限</div>
                </el-form-item>
                <el-form-item label="用户配额">
                  <div style="display: flex; align-items: center; gap: 12px;">
                    <el-input v-model="privateQuotaUserDisplay" :maxlength="32" placeholder="如 100m, 5g" style="width: 200px;" />
                    <span style="color: #999;">{{ formatSize(settings.privateQuotaUser) }}</span>
                  </div>
                  <div class="field-hint">每个用户的私有文件存储上限</div>
                </el-form-item>
              </el-form>
            </el-card>
          </el-col>

          <el-col :span="12" :sm="12" :xs="24" style="margin-bottom: 16px;">
            <el-card>
              <template #header><span>临时文件配置</span></template>
              <el-form label-width="120px">
                <el-form-item label="启用临时文件">
                  <el-switch v-model="settings.tempFiles.enabled" />
                  <div class="field-hint">开启后无需登录即可上传文件</div>
                </el-form-item>
                <el-form-item label="存储路径">
                  <el-input v-model="settings.tempFiles.path" :maxlength="1024" placeholder="临时文件存储路径" />
                  <div class="field-hint">临时文件的存储目录</div>
                </el-form-item>
                <el-form-item label="全局配额">
                  <div style="display: flex; align-items: center; gap: 12px;">
                    <el-input v-model="tempQuotaGlobalDisplay" :maxlength="32" placeholder="如 10g, 100g" style="width: 200px;" />
                    <span style="color: #999;">{{ formatSize(settings.tempFilesQuotaGlobal) }}</span>
                  </div>
                  <div class="field-hint">所有临时文件的总存储上限</div>
                </el-form-item>
                <el-form-item label="IP 配额">
                  <div style="display: flex; align-items: center; gap: 12px;">
                    <el-input v-model="tempQuotaPerIPDisplay" :maxlength="32" placeholder="如 500m, 2g" style="width: 200px;" />
                    <span style="color: #999;">{{ formatSize(settings.tempFilesQuotaPerIP) }}</span>
                  </div>
                  <div class="field-hint">每个 IP 的临时文件存储上限</div>
                </el-form-item>
                <el-form-item label="默认过期天数">
                  <el-input-number v-model="settings.tempFiles.defaultExpireDays" :min="1" :max="365" />
                  <div class="field-hint">临时文件默认的有效天数</div>
                </el-form-item>
                <el-form-item label="下载后删除">
                  <el-switch v-model="settings.tempFiles.deleteOnDownload" />
                  <div class="field-hint">开启后文件被下载一次即自动删除</div>
                </el-form-item>
              </el-form>
            </el-card>
          </el-col>

          <el-col :span="12" :sm="12" :xs="24" style="margin-bottom: 16px;">
            <el-card>
              <template #header><span>上传配置</span></template>
              <el-form label-width="120px">
                <el-form-item label="分片大小">
                  <div style="display: flex; align-items: center; gap: 12px;">
                    <el-input v-model="chunkSizeDisplay" :maxlength="32" placeholder="如 10m, 1g" style="width: 200px;" />
                    <span style="color: #999;">{{ formatSize(settings.upload.chunkSize) }}</span>
                  </div>
                  <div class="field-hint">文件分块上传的块大小，建议 5m-10m</div>
                </el-form-item>
                <el-form-item label="最大文件大小">
                  <div style="display: flex; align-items: center; gap: 12px;">
                    <el-input v-model="maxFileSizeDisplay" :maxlength="32" placeholder="如 2g, 10g, 无限制" style="width: 200px;" />
                    <span style="color: #999;">
                      {{ settings.upload.maxFileSize === 0 ? '无限制' : formatSize(settings.upload.maxFileSize) }}
                    </span>
                  </div>
                  <div class="field-hint">允许上传的单文件最大体积，0 表示不限制</div>
                </el-form-item>
              </el-form>
            </el-card>
          </el-col>

          <el-col :span="12" :sm="12" :xs="24" style="margin-bottom: 16px;">
            <el-card>
              <template #header><span>URL 上传配置</span></template>
              <el-form label-width="120px">
                <el-form-item label="启用 URL 上传">
                  <el-switch v-model="settings.upload.urlUpload.enabled" />
                  <div class="field-hint">开启后可通过远程 URL 下载文件到服务器</div>
                </el-form-item>
                <el-form-item label="跳过证书验证">
                  <el-switch v-model="settings.upload.urlUpload.insecureSkipVerify" />
                  <div class="field-hint">跳过 HTTPS/FTPS 的 TLS 证书验证</div>
                </el-form-item>
              </el-form>
            </el-card>
          </el-col>
        </el-row>
      </el-tab-pane>

      <!-- 服务配置 -->
      <el-tab-pane name="service" label="服务配置">
        <el-row :gutter="16">
          <el-col :span="12" :sm="12" :xs="24" style="margin-bottom: 16px;">
            <el-card>
              <template #header><span>WEB 服务</span></template>
              <el-form label-width="120px">
                <el-form-item label="监听地址">
                  <el-select v-model="settings.server.host">
                    <el-option v-for="opt in ipOptions" :key="opt.value" :label="opt.label" :value="opt.value" />
                  </el-select>
                  <div class="field-hint">服务器监听的 IP 地址。<code>0.0.0.0</code> 表示监听所有网卡</div>
                </el-form-item>
                <el-divider />
                <span style="font-weight: 600; color: #909399;">HTTP</span>
                <div style="width: 100%;">
                  <el-form-item label="启用 HTTP">
                    <el-switch v-model="settings.server.http.enabled" />
                    <div class="field-hint">关闭后 HTTP 端口将不可用</div>
                  </el-form-item>
                  <el-form-item label="HTTP 端口" v-if="settings.server.http.enabled">
                    <el-input-number v-model="settings.server.http.port" :min="1" :max="65535" />
                    <div class="field-hint">HTTP 服务监听端口，常用：8080（开发）、80（生产）</div>
                  </el-form-item>
                </div>
                <el-divider />
                <span style="font-weight: 600; color: #909399;">HTTPS</span>
                <div style="width: 100%;">
                  <el-form-item label="启用 HTTPS">
                    <el-switch v-model="settings.server.https.enabled" />
                    <div class="field-hint">启用后可通过 HTTPS 加密访问</div>
                  </el-form-item>
                  <el-form-item label="HTTPS 端口" v-if="settings.server.https.enabled">
                    <el-input-number v-model="settings.server.https.port" :min="1" :max="65535" />
                    <div class="field-hint">HTTPS 服务监听端口，默认 8443</div>
                  </el-form-item>
                </div>
              </el-form>
            </el-card>
          </el-col>

          <el-col :span="12" :sm="12" :xs="24" style="margin-bottom: 16px;">
            <el-card>
              <template #header><span>FTP 服务</span></template>
              <el-form label-width="120px">
                <el-form-item label="启用 FTP">
                  <el-switch v-model="settings.server.ftp.enabled" />
                  <div class="field-hint">开启后可通过 FTP 协议访问共享文件</div>
                </el-form-item>
                <el-form-item label="FTP 端口" v-if="settings.server.ftp.enabled">
                  <el-input-number v-model="settings.server.ftp.port" :min="1" :max="65535" />
                  <div class="field-hint">FTP 端口，默认 21</div>
                </el-form-item>
              </el-form>
            </el-card>
          </el-col>

          <el-col :span="12" :sm="12" :xs="24" style="margin-bottom: 16px;">
            <el-card>
              <template #header><span>FTPS 服务</span></template>
              <el-form label-width="120px">
                <el-form-item label="启用 FTPS">
                  <el-switch v-model="settings.server.ftps.enabled" />
                  <div class="field-hint">开启后可通过 FTPS（FTP over TLS）安全访问</div>
                </el-form-item>
                <el-form-item label="FTPS 端口" v-if="settings.server.ftps.enabled">
                  <el-input-number v-model="settings.server.ftps.port" :min="1" :max="65535" />
                  <div class="field-hint">FTPS 端口，默认 990</div>
                </el-form-item>
              </el-form>
            </el-card>
          </el-col>

          <el-col :span="12" :sm="12" :xs="24" style="margin-bottom: 16px;">
            <el-card>
              <template #header><span>WebDAV 服务</span></template>
              <el-alert type="info" :show-icon="false" :closable="false" style="margin-bottom: 16px;">
                <span class="field-hint">WebDAV 通过 HTTP/HTTPS 端口提供访问，无需额外端口配置。</span>
              </el-alert>
              <el-form label-width="120px">
                <el-form-item label="启用 WebDAV">
                  <el-switch v-model="settings.server.webdav.enabled" />
                  <div class="field-hint">开启后可通过 WebDAV 客户端浏览文件</div>
                </el-form-item>
                <el-form-item label="公开用户名" v-if="settings.server.webdav.enabled">
                  <el-input v-model="settings.account.anonymous.username" placeholder="public" />
                  <div class="field-hint">WebDAV/FTP 公开目录的默认用户名</div>
                </el-form-item>
              </el-form>
            </el-card>
          </el-col>

          <el-col :span="12" :sm="12" :xs="24" style="margin-bottom: 16px;">
            <el-card>
              <template #header><span>TLS 证书</span></template>
              <el-alert type="info" :show-icon="false" :closable="false" style="margin-bottom: 16px;">
                <span class="field-hint">TLS 证书由 HTTPS 和 FTPS 共享使用，配置一次即可。</span>
              </el-alert>
              <el-form label-width="120px">
                <el-form-item label="证书文件">
                  <div style="display: flex; align-items: center; gap: 12px;">
                    <el-input v-model="settings.server.tls.certFile" placeholder="未配置" readonly style="width: 250px;" />
                    <el-upload
                      :show-file-list="false"
                      accept=".pem,.crt"
                      :http-request="handleCertUpload"
                    >
                      <el-button size="small">上传</el-button>
                    </el-upload>
                  </div>
                  <div class="field-hint">上传 .pem 或 .crt 格式的证书文件</div>
                </el-form-item>
                <el-form-item label="密钥文件">
                  <div style="display: flex; align-items: center; gap: 12px;">
                    <el-input v-model="settings.server.tls.keyFile" placeholder="未配置" readonly style="width: 250px;" />
                    <el-upload
                      :show-file-list="false"
                      accept=".key"
                      :http-request="handleKeyUpload"
                    >
                      <el-button size="small">上传</el-button>
                    </el-upload>
                  </div>
                  <div class="field-hint">上传 .key 格式的密钥文件</div>
                </el-form-item>
              </el-form>
            </el-card>
          </el-col>
        </el-row>
      </el-tab-pane>

      <!-- 存储配置 -->
      <el-tab-pane name="storage" label="存储配置">
        <el-row :gutter="16">
          <el-col :span="12" :sm="12" :xs="24">
            <el-card>
              <template #header><span>文件索引配置</span></template>
              <el-form label-width="140px">
                <el-form-item label="定时扫描间隔">
                  <el-input v-model="settings.scanCronExpression" placeholder="如 0 1 * * *（每天凌晨1点）" />
                  <div class="field-hint">Cron 表达式，默认 <code>0 1 * * *</code>（每天凌晨 1:00）</div>
                </el-form-item>
              </el-form>
            </el-card>
          </el-col>
        </el-row>
      </el-tab-pane>

      <!-- 预览配置 -->
      <el-tab-pane name="preview" label="预览配置">
        <el-row :gutter="16">
          <el-col :span="12" :sm="12" :xs="24">
            <el-card>
              <template #header><span>预览配置</span></template>
              <el-form label-width="140px">
                <el-form-item label="MIME 类型">
                  <el-input
                    v-model="settings.preview.allowMimes"
                    :maxlength="1024"
                    type="textarea"
                    placeholder="text/*,image/*,application/pdf,application/json"
                    :rows="2"
                  />
                  <div class="field-hint">允许预览的 MIME 类型，逗号分隔，支持通配符</div>
                </el-form-item>
                <el-form-item label="文件扩展名">
                  <el-input
                    v-model="settings.preview.allowExts"
                    :maxlength="1024"
                    type="textarea"
                    placeholder="txt,md,log,json,html,css,js"
                    :rows="2"
                  />
                  <div class="field-hint">允许预览的文件扩展名，逗号分隔</div>
                </el-form-item>
                <el-form-item label="最大内联大小">
                  <div style="display: flex; align-items: center; gap: 12px;">
                    <el-input v-model="maxInlineSizeDisplay" :maxlength="32" placeholder="如 1m, 2m" style="width: 200px;" />
                    <span style="color: #999;">{{ formatSize(settings.preview.maxInlineSize) }}</span>
                  </div>
                  <div class="field-hint">浏览器直接预览的文件大小上限</div>
                </el-form-item>
                <el-form-item label="文本分块大小">
                  <div style="display: flex; align-items: center; gap: 12px;">
                    <el-input v-model="textChunkSizeDisplay" placeholder="如 100k, 200k" style="width: 200px;" />
                    <span style="color: #999;">{{ formatSize(settings.preview.textChunkSize) }}</span>
                  </div>
                  <div class="field-hint">预览大文本文件时分块读取的大小</div>
                </el-form-item>
              </el-form>
            </el-card>
          </el-col>
        </el-row>
      </el-tab-pane>

      <!-- 数据库 -->
      <el-tab-pane name="database" label="数据库">
        <el-card>
          <el-alert type="info" :show-icon="false" :closable="false" class="migration-hint">
            <template #title><span class="migration-title">数据迁移说明</span></template>
            <ul class="migration-list">
              <li>修改数据库类型（如 SQLite → MySQL）或目标地址时会触发数据迁移</li>
              <li>迁移过程中原数据库保持不变，可随时回滚</li>
              <li>迁移完成后配置即时生效</li>
              <li><strong>风险提示</strong>：迁移存在一定风险，建议提前备份重要数据</li>
            </ul>
          </el-alert>

          <el-divider />

          <el-form ref="formRef" :model="settings" label-width="120px">
            <el-form-item label="数据库类型">
              <el-radio-group v-model="settings.database.driver">
                <el-radio label="sqlite">SQLite</el-radio>
                <el-radio label="mysql">MySQL</el-radio>
                <el-radio label="postgres">PostgreSQL</el-radio>
              </el-radio-group>
              <div class="field-hint">
                <strong>SQLite</strong>：轻量级，文件存储，适合小型部署<br>
                <strong>MySQL</strong>：适合大规模应用，需要 MySQL 5.7+<br>
                <strong>PostgreSQL</strong>：功能丰富，适合企业级应用，需要 PostgreSQL 10+
              </div>
            </el-form-item>

            <el-form-item v-if="settings.database.driver === 'sqlite'" label="数据库文件" prop="database.dsn">
              <el-input v-model="settings.database.dsn" :maxlength="1024" placeholder="fuzhan.db" />
              <div class="field-hint">SQLite 数据库文件路径</div>
            </el-form-item>

            <template v-if="settings.database.driver === 'mysql'">
              <el-form-item label="主机地址" prop="database.mysqlHost">
                <el-input v-model="settings.database.mysqlHost" :maxlength="255" placeholder="localhost" />
              </el-form-item>
              <el-form-item label="端口" prop="database.mysqlPort">
                <el-input-number v-model="settings.database.mysqlPort" :min="1" :max="65535" />
              </el-form-item>
              <el-form-item label="用户名" prop="database.mysqlUser">
                <el-input v-model="settings.database.mysqlUser" :maxlength="64" placeholder="root" />
              </el-form-item>
              <el-form-item label="密码">
                <el-input v-model="settings.database.mysqlPassword" :maxlength="128" type="password" placeholder="输入密码" show-password />
              </el-form-item>
              <el-form-item label="数据库名" prop="database.mysqlDatabase">
                <el-input v-model="settings.database.mysqlDatabase" :maxlength="64" placeholder="fuzhan" />
              </el-form-item>
              <el-form-item>
                <el-button
                  :loading="testingDb"
                  :type="dbTestResult?.success === true ? 'success' : dbTestResult?.success === false ? 'danger' : ''"
                  @click="testDbConnection"
                >
                  {{ testingDb ? '测试中...' : dbTestResult ? (dbTestResult.success ? '重新测试' : '重试') : '测试连接' }}
                </el-button>
                <span v-if="dbTestResult" class="test-result" :class="dbTestResult.success ? 'success' : 'error'">
                  {{ dbTestResult.success ? '连接成功' : dbTestResult.error }}
                </span>
              </el-form-item>
            </template>

            <template v-if="settings.database.driver === 'postgres'">
              <el-form-item label="主机地址" prop="database.postgresHost">
                <el-input v-model="settings.database.postgresHost" :maxlength="255" placeholder="localhost" />
              </el-form-item>
              <el-form-item label="端口" prop="database.postgresPort">
                <el-input-number v-model="settings.database.postgresPort" :min="1" :max="65535" />
              </el-form-item>
              <el-form-item label="用户名" prop="database.postgresUser">
                <el-input v-model="settings.database.postgresUser" :maxlength="64" placeholder="postgres" />
              </el-form-item>
              <el-form-item label="密码">
                <el-input v-model="settings.database.postgresPassword" :maxlength="128" type="password" placeholder="输入密码" show-password />
              </el-form-item>
              <el-form-item label="数据库名" prop="database.postgresDatabase">
                <el-input v-model="settings.database.postgresDatabase" :maxlength="64" placeholder="fuzhan" />
              </el-form-item>
              <el-form-item>
                <el-button
                  :loading="testingDb"
                  :type="dbTestResult?.success === true ? 'success' : dbTestResult?.success === false ? 'danger' : ''"
                  @click="testDbConnection"
                >
                  {{ testingDb ? '测试中...' : dbTestResult ? (dbTestResult.success ? '重新测试' : '重试') : '测试连接' }}
                </el-button>
                <span v-if="dbTestResult" class="test-result" :class="dbTestResult.success ? 'success' : 'error'">
                  {{ dbTestResult.success ? '连接成功' : dbTestResult.error }}
                </span>
              </el-form-item>
            </template>
          </el-form>
        </el-card>
      </el-tab-pane>

      <!-- 访问控制 -->
      <el-tab-pane name="access" label="访问控制">
        <el-row :gutter="16">
          <el-col :span="12" :sm="12" :xs="24" style="margin-bottom: 16px;">
            <el-card>
              <template #header><span>IP 访问控制</span></template>
              <el-form label-width="120px">
                <el-form-item label="访问模式">
                  <el-radio-group v-model="settings.openApi.ipAccessMode">
                    <el-radio label="allow">白名单模式</el-radio>
                    <el-radio label="deny">黑名单模式</el-radio>
                    <el-radio label="none">不限制</el-radio>
                  </el-radio-group>
                  <div class="field-hint">白名单和黑名单不能同时生效</div>
                </el-form-item>
                <el-form-item label="IP 列表" v-if="settings.openApi.ipAccessMode !== 'none'">
                  <el-input
                    v-model="ipAccessListDisplay"
                    type="textarea"
                    :placeholder="settings.openApi.ipAccessMode === 'allow' ? '每行一个 IP 或 CIDR，如 192.168.1.0/24' : '每行一个 IP 或 CIDR'"
                    :rows="5"
                  />
                  <div class="field-hint">{{ settings.openApi.ipAccessMode === 'allow' ? '白名单中的 IP 允许访问' : '黑名单中的 IP 将被拒绝访问' }}</div>
                </el-form-item>
                <el-form-item label="频率限制">
                  <el-switch v-model="settings.openApi.rateLimitEnabled" />
                  <div class="field-hint">限制调用频率，防止滥用</div>
                </el-form-item>
                <el-form-item label="请求频率" v-if="settings.openApi.rateLimitEnabled">
                  <div style="display: flex; align-items: center; gap: 12px;">
                    <el-input-number v-model="settings.openApi.requestsPerMinute" :min="1" :max="10000" />
                    <span>次/分钟</span>
                  </div>
                </el-form-item>
              </el-form>
            </el-card>
          </el-col>

          <el-col :span="12" :sm="12" :xs="24" style="margin-bottom: 16px;">
            <el-card>
              <template #header><span>文件访问控制</span></template>
              <el-form label-width="120px">
                <el-form-item label="允许的扩展名">
                  <el-input
                    v-model="extensionsDisplay"
                    type="textarea"
                    placeholder="每行一个扩展名，如 txt、pdf、jpg"
                    :rows="6"
                  />
                  <div class="field-hint">留空允许所有扩展名；每行输入一个允许的扩展名，不含点号</div>
                </el-form-item>
              </el-form>
            </el-card>
          </el-col>
        </el-row>
      </el-tab-pane>
    </el-tabs>

    <div class="actions-bar">
      <div class="actions-inner">
        <el-button @click="loadSettings">重置</el-button>
        <el-button type="primary" :loading="saving" @click="saveSettings">保存配置</el-button>
      </div>
    </div>

    <!-- 数据库迁移向导 -->
    <MigrationWizard
      v-model:show="showMigrationWizard"
      :initial-type="settings.database.driver"
      :initial-config="settings.database"
      @migrated="handleMigrated"
    />
  </div>
</template>

<script setup>
import { ref, reactive, onMounted, computed } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { NumberUtils } from '@/utils'
import { formatErrorMessage } from '@/utils/error'
import { ConfigApi, DatabaseApi, SetupApi, SystemApi } from '@/api'
import store from '@/store'
import MigrationWizard from '@/components/settings/migration/MigrationWizard.vue'

const message = ElMessage
const dialog = {
  warning: (opts) => ElMessageBox.confirm(opts.content, opts.title, {
    confirmButtonText: opts.positiveText || '确定',
    cancelButtonText: opts.negativeText || '取消',
    type: 'warning',
    closeOnClickModal: !!opts.onMaskClick
  }).then(() => { opts.onPositiveClick?.() }).catch(() => { opts.onNegativeClick?.() })
}
const saving = ref(false)
const activeTab = ref('basic')
const showMigrationWizard = ref(false)
const formRef = ref(null)

// 保存原始配置用于检测变更
const originalConfig = ref(null)
const originalServerConfig = ref(null)

// 测试连接状态
const testingDb = ref(false)
const dbTestResult = ref(null)

// 校验规则
const rules = {
  appName: {
    required: true,
    message: '请输入应用名称',
    trigger: ['blur', 'input']
  },
  'server.host': {
    required: true,
    message: '请选择监听地址',
    trigger: ['blur', 'change']
  },
  'server.http.port': {
    required: true,
    type: 'number',
    message: '请输入有效端口 (1-65535)',
    trigger: ['blur', 'change']
  },
  'database.dsn': {
    required: true,
    message: '请输入数据库文件路径',
    trigger: ['blur', 'input']
  },
  'database.mysqlHost': {
    required: true,
    message: '请输入 MySQL 主机地址',
    trigger: ['blur', 'input']
  },
  'database.mysqlPort': {
    required: true,
    type: 'number',
    message: '请输入有效端口 (1-65535)',
    trigger: ['blur', 'change']
  },
  'database.mysqlUser': {
    required: true,
    message: '请输入 MySQL 用户名',
    trigger: ['blur', 'input']
  },
  'database.mysqlDatabase': {
    required: true,
    message: '请输入 MySQL 数据库名',
    trigger: ['blur', 'input']
  },
  'database.postgresHost': {
    required: true,
    message: '请输入 PostgreSQL 主机地址',
    trigger: ['blur', 'input']
  },
  'database.postgresPort': {
    required: true,
    type: 'number',
    message: '请输入有效端口 (1-65535)',
    trigger: ['blur', 'change']
  },
  'database.postgresUser': {
    required: true,
    message: '请输入 PostgreSQL 用户名',
    trigger: ['blur', 'input']
  },
  'database.postgresDatabase': {
    required: true,
    message: '请输入 PostgreSQL 数据库名',
    trigger: ['blur', 'input']
  }
}

// 文件扩展名显示转换（数组 <-> 文本框）
const extensionsDisplay = computed({
  get: () => (settings.allowedExtensions || []).join('\n'),
  set: (val) => {
    settings.allowedExtensions = val.split('\n').map(s => s.trim()).filter(Boolean)
  }
})

// IP 访问列表显示转换（数组 <-> 文本框）
const ipAccessListDisplay = computed({
  get: () => {
    if (settings.openApi.ipAccessMode === 'allow') return (settings.openApi.ipWhitelist || '').split('\n').filter(s => s.trim()).join('\n')
    if (settings.openApi.ipAccessMode === 'deny') return (settings.openApi.ipBlacklist || '').split('\n').filter(s => s.trim()).join('\n')
    return ''
  },
  set: (val) => {
    const list = val.split('\n').map(s => s.trim()).filter(Boolean)
    if (settings.openApi.ipAccessMode === 'allow') {
      settings.openApi.ipWhitelist = list.join('\n')
    } else if (settings.openApi.ipAccessMode === 'deny') {
      settings.openApi.ipBlacklist = list.join('\n')
    }
  }
})

// 单位输入的双向绑定
const privateQuotaGlobalDisplay = computed({
  get: () => formatToUnit(settings.privateQuotaGlobal),
  set: (val) => { settings.privateQuotaGlobal = NumberUtils.parseFileSize(val) }
})
const privateQuotaUserDisplay = computed({
  get: () => formatToUnit(settings.privateQuotaUser),
  set: (val) => { settings.privateQuotaUser = NumberUtils.parseFileSize(val) }
})
const tempQuotaGlobalDisplay = computed({
  get: () => formatToUnit(settings.tempFilesQuotaGlobal),
  set: (val) => { settings.tempFilesQuotaGlobal = NumberUtils.parseFileSize(val) }
})
const tempQuotaPerIPDisplay = computed({
  get: () => formatToUnit(settings.tempFilesQuotaPerIP),
  set: (val) => { settings.tempFilesQuotaPerIP = NumberUtils.parseFileSize(val) }
})
const chunkSizeDisplay = computed({
  get: () => formatToUnit(settings.upload.chunkSize),
  set: (val) => { settings.upload.chunkSize = NumberUtils.parseFileSize(val) }
})
const maxFileSizeDisplay = computed({
  get: () => settings.upload.maxFileSize === 0 ? '无限制' : formatToUnit(settings.upload.maxFileSize),
  set: (val) => {
    if (val === '无限制' || val === '0') {
      settings.upload.maxFileSize = 0
    } else {
      settings.upload.maxFileSize = NumberUtils.parseFileSize(val)
    }
  }
})
const maxInlineSizeDisplay = computed({
  get: () => formatToUnit(settings.preview.maxInlineSize),
  set: (val) => { settings.preview.maxInlineSize = NumberUtils.parseFileSize(val) }
})
const textChunkSizeDisplay = computed({
  get: () => formatToUnit(settings.preview.textChunkSize),
  set: (val) => { settings.preview.textChunkSize = NumberUtils.parseFileSize(val) }
})

// 格式化字节为人类可读单位
const formatToUnit = (bytes) => {
  if (!bytes || bytes === 0) return '0'
  if (bytes >= 1024 * 1024 * 1024 * 1024) {
    return (bytes / (1024 * 1024 * 1024 * 1024)).toFixed(1) + 't'
  }
  if (bytes >= 1024 * 1024 * 1024) {
    return (bytes / (1024 * 1024 * 1024)).toFixed(1) + 'g'
  }
  if (bytes >= 1024 * 1024) {
    return (bytes / (1024 * 1024)).toFixed(1) + 'm'
  }
  if (bytes >= 1024) {
    return (bytes / 1024).toFixed(1) + 'k'
  }
  return bytes.toString()
}

const ipOptions = ref([])

// 加载网络接口列表
const loadNetworkInterfaces = async () => {
  try {
    const res = await SetupApi.getNetworkInterfaces()
    if (res.success && res.data) {
      ipOptions.value = res.data.map(item => ({
        label: `${item.name} (${item.ip})`,
        value: item.ip
      }))
    }
  } catch (e) {
    console.error('获取网络接口失败:', e)
  }
}

// 证书上传处理（el-upload http-request）
const handleCertUpload = async ({ file, onSuccess, onError }) => {
  try {
    const res = await SystemApi.uploadCert(file)
    if (res.success && res.data?.path) {
      settings.server.tls.certFile = res.data.path
      message.success('证书上传成功')
    } else {
      message.error(res.message || '证书上传失败')
    }
    onSuccess?.(res)
  } catch (e) {
    message.error('证书上传失败')
    onError?.(e)
  }
}

// 密钥上传处理（el-upload http-request）
const handleKeyUpload = async ({ file, onSuccess, onError }) => {
  try {
    const res = await SystemApi.uploadKey(file)
    if (res.success && res.data?.path) {
      settings.server.tls.keyFile = res.data.path
      message.success('密钥上传成功')
    } else {
      message.error(res.message || '密钥上传失败')
    }
    onSuccess?.(res)
  } catch (e) {
    message.error('密钥上传失败')
    onError?.(e)
  }
}

const settings = reactive({
  appName: '',
  account: {
    anonymous: { username: 'public' },
    reservedUsernames: []
  },
  server: {
    host: '0.0.0.0',
    http: { enabled: true, port: 8080 },
    https: { enabled: false, port: 8443 },
    ftp: { enabled: false, port: 21 },
    ftps: { enabled: false, port: 990 },
    webdav: { enabled: false },
    tls: { certFile: '', keyFile: '' }
  },
  database: {
    driver: 'sqlite',
    dsn: '',
    mysqlHost: 'localhost',
    mysqlPort: 3306,
    mysqlUser: 'root',
    mysqlPassword: '',
    mysqlDatabase: 'fuzhan',
    postgresHost: 'localhost',
    postgresPort: 5432,
    postgresUser: 'postgres',
    postgresPassword: '',
    postgresDatabase: 'fuzhan',
  },
  rootDirs: [],
  allowedExtensions: [],
  privateFiles: {
    enabled: false,
    path: ''
  },
  privateQuotaGlobal: 0,
  privateQuotaUser: 0,
  upload: {
    chunkSize: 10485760,
    maxFileSize: 17179869184,
    urlUpload: { enabled: false, insecureSkipVerify: false }
  },
  tempFiles: {
    enabled: true,
    path: '',
    defaultExpireDays: 7,
    deleteOnDownload: true
  },
  tempFilesQuotaGlobal: 0,
  tempFilesQuotaPerIP: 0,
  preview: {
    allowMimes: '',
    allowExts: '',
    maxInlineSize: 1048576,
    textChunkSize: 102400
  },
  openApi: {
    enabled: false,
    ipAccessMode: 'none',
    ipWhitelist: '',
    ipBlacklist: '',
    rateLimitEnabled: true,
    requestsPerMinute: 60
  },
  scanCronExpression: '0 1 * * *',
})

const formatSize = (bytes) => NumberUtils.formatFileSize(bytes)

// 测试数据库连接
const testDbConnection = async () => {
  testingDb.value = true
  dbTestResult.value = null

  let dsn = settings.database.dsn
  if (settings.database.driver === 'mysql') {
    dsn = buildMysqlDsn()
  } else if (settings.database.driver === 'postgres') {
    dsn = buildPostgresDsn()
  }

  try {
    const result = await DatabaseApi.testConnection({
      driver: settings.database.driver,
      dsn
    })
    if (result.data?.connected) {
      dbTestResult.value = { success: true, info: result.data }
      message.success('连接成功')
    } else {
      dbTestResult.value = {
        success: false,
        error: result.data?.errorInfo?.message || '连接失败'
      }
      message.error(result.data?.errorInfo?.message || '连接失败')
    }
  } catch (err) {
    dbTestResult.value = { success: false, error: formatErrorMessage(err, '连接失败') }
    message.error(formatErrorMessage(err, '连接测试失败'))
  } finally {
    testingDb.value = false
  }
}

const addDir = () => {
  settings.rootDirs.push({ path: '', name: '' })
}

const removeDir = (index) => {
  settings.rootDirs.splice(index, 1)
}

// 解析 MySQL DSN
const parseMysqlDsn = (dsn) => {
  const match = dsn.match(/([^:@]+):([^@]*)@tcp\(([^:]+):(\d+)\)\/([^?]+)/)
  if (match) {
    settings.database.mysqlUser = match[1]
    settings.database.mysqlPassword = match[2]
    settings.database.mysqlHost = match[3]
    settings.database.mysqlPort = parseInt(match[4])
    settings.database.mysqlDatabase = match[5]
  }
}

// 解析 PostgreSQL DSN
const parsePostgresDsn = (dsn) => {
  const getParam = (str, key) => {
    const match = str.match(new RegExp(`${key}=(\\S+)`))
    return match ? match[1] : ''
  }
  settings.database.postgresHost = getParam(dsn, 'host') || 'localhost'
  settings.database.postgresPort = parseInt(getParam(dsn, 'port')) || 5432
  settings.database.postgresUser = getParam(dsn, 'user') || 'postgres'
  settings.database.postgresPassword = getParam(dsn, 'password')
  settings.database.postgresDatabase = getParam(dsn, 'dbname') || 'fuzhan'
}

// 构建 MySQL DSN
const buildMysqlDsn = () => {
  const { mysqlHost, mysqlPort, mysqlUser, mysqlPassword, mysqlDatabase } = settings.database
  const passwordPart = mysqlPassword ? `${mysqlPassword}@` : '@'
  return `${mysqlUser}:${passwordPart}tcp(${mysqlHost}:${mysqlPort})/${mysqlDatabase}`
}

// 构建 PostgreSQL DSN
const buildPostgresDsn = () => {
  const { postgresHost, postgresPort, postgresUser, postgresPassword, postgresDatabase } = settings.database
  const parts = [
    `host=${postgresHost}`,
    `port=${postgresPort}`,
    `user=${postgresUser}`,
    `dbname=${postgresDatabase}`
  ]
  if (postgresPassword) {
    parts.push(`password=${postgresPassword}`)
  }
  return parts.join(' ')
}

const loadSettings = async () => {
  try {
    const data = await ConfigApi.get()
    if (data.success && data.data) {
      const cfg = data.data
      settings.appName = cfg.app?.name || ''
      settings.server.host = cfg.server?.host || '0.0.0.0'
      settings.server.http = {
        enabled: cfg.server?.http?.enabled ?? true,
        port: cfg.server?.http?.port || 8080
      }
      settings.server.https = {
        enabled: cfg.server?.https?.enabled ?? false,
        port: cfg.server?.https?.port || 8443
      }
      settings.server.ftp = {
        enabled: cfg.server?.ftp?.enabled ?? false,
        port: cfg.server?.ftp?.port || 21
      }
      settings.server.ftps = {
        enabled: cfg.server?.ftps?.enabled ?? false,
        port: cfg.server?.ftps?.port || 990
      }
      settings.account = {
        anonymous: { username: cfg.account?.anonymous?.username || 'public' },
        reservedUsernames: cfg.account?.reservedUsernames || []
      }
      settings.server.webdav = {
        enabled: cfg.server?.webdav?.enabled ?? false
      }
      settings.database.driver = cfg.database?.driver || 'sqlite'
      settings.database.dsn = cfg.database?.dsn || ''

      // 解析 MySQL DSN
      if (settings.database.driver === 'mysql' && cfg.database?.dsn) {
        parseMysqlDsn(cfg.database.dsn)
      }
      // 解析 PostgreSQL DSN
      if (settings.database.driver === 'postgres' && cfg.database?.dsn) {
        parsePostgresDsn(cfg.database.dsn)
      }
      settings.rootDirs = cfg.rootDirs?.map(d => ({ path: d.fullPath, name: d.name })) || []
      settings.allowedExtensions = cfg.allowedExtensions || []

      // 私有文件配置
      settings.privateFiles.enabled = cfg.privateFiles?.enabled ?? false
      settings.privateFiles.path = cfg.privateFiles?.path || ''
      settings.privateQuotaGlobal = cfg.privateFiles?.quotaGlobal || 0
      settings.privateQuotaUser = cfg.privateFiles?.quotaPerUser || 0

      // 临时文件配置
      settings.tempFiles.enabled = cfg.tempFiles?.enabled ?? true
      settings.tempFiles.path = cfg.tempFiles?.path || ''
      settings.tempFiles.defaultExpireDays = cfg.tempFiles?.defaultExpireDays || 7
      settings.tempFiles.deleteOnDownload = cfg.tempFiles?.deleteOnDownload ?? true
      settings.tempFilesQuotaGlobal = cfg.tempFiles?.quotaGlobal || 0
      settings.tempFilesQuotaPerIP = cfg.tempFiles?.quotaPerIP || 0

      settings.upload = cfg.upload || { chunkSize: 10485760, maxFileSize: 17179869184, urlUpload: { enabled: false, insecureSkipVerify: false } }

      // 预览配置
      settings.preview = {
        allowMimes: cfg.preview?.allowMimes || '',
        allowExts: cfg.preview?.allowExts || '',
        maxInlineSize: NumberUtils.parseFileSize(cfg.preview?.maxInlineSize || '1M'),
        textChunkSize: NumberUtils.parseFileSize(cfg.preview?.textChunkSize || '100K')
      }

      // Open API 配置
      settings.openApi = {
        enabled: cfg.openApi?.enabled ?? false,
        ipAccessMode: cfg.openApi?.ipAccessMode || 'none',
        ipWhitelist: (cfg.openApi?.ipWhitelist || []).join('\n'),
        ipBlacklist: (cfg.openApi?.ipBlacklist || []).join('\n'),
        rateLimitEnabled: cfg.openApi?.rateLimitEnabled ?? true,
        requestsPerMinute: cfg.openApi?.requestsPerMinute || 60
      }

      // 文件索引配置
      settings.scanCronExpression = cfg.index?.scanCronExpression || '0 1 * * *'

      // 保存原始数据库配置用于变更检测
      originalConfig.value = { ...settings.database }
      originalServerConfig.value = {
        host: settings.server.host,
        httpPort: settings.server.http.port
      }
    }
  } catch (e) {
    console.error('加载设置失败', e)
    message.error('加载设置失败')
  }
}

// 检测数据库配置是否有变更
const detectDatabaseChange = () => {
  if (!originalConfig.value) return false
  const orig = originalConfig.value
  const curr = settings.database

  if (orig.driver !== curr.driver) return true
  if (curr.driver === 'sqlite' && orig.dsn !== curr.dsn) return true
  if (curr.driver === 'mysql') {
    if (orig.mysqlHost !== curr.mysqlHost) return true
    if (orig.mysqlPort !== curr.mysqlPort) return true
    if (orig.mysqlUser !== curr.mysqlUser) return true
    if (orig.mysqlPassword !== curr.mysqlPassword) return true
    if (orig.mysqlDatabase !== curr.mysqlDatabase) return true
  }
  if (curr.driver === 'postgres') {
    if (orig.postgresHost !== curr.postgresHost) return true
    if (orig.postgresPort !== curr.postgresPort) return true
    if (orig.postgresUser !== curr.postgresUser) return true
    if (orig.postgresPassword !== curr.postgresPassword) return true
    if (orig.postgresDatabase !== curr.postgresDatabase) return true
  }
  return false
}

// 等待服务就绪后导航
const pollServerHealth = (url, maxRetries, interval, onReady) => {
  let retries = 0
  const check = () => {
    retries++
    const controller = new AbortController()
    const timeoutId = setTimeout(() => controller.abort(), 3000)
    fetch(url + '/api/v1/health', { mode: 'no-cors', signal: controller.signal })
      .then(() => { clearTimeout(timeoutId); onReady() })
      .catch(() => {
        clearTimeout(timeoutId)
        if (retries < maxRetries) {
          setTimeout(check, interval)
        } else {
          onReady()
        }
      })
  }
  check()
}

// 执行保存配置
const doSaveSettings = async () => {
  saving.value = true
  try {
    let finalDsn = settings.database.dsn
    if (settings.database.driver === 'mysql') {
      finalDsn = buildMysqlDsn()
    } else if (settings.database.driver === 'postgres') {
      finalDsn = buildPostgresDsn()
    }

    const configData = {
      app: { name: settings.appName },
      account: {
        anonymous: { username: settings.account.anonymous.username },
        reservedUsernames: settings.account.reservedUsernames
      },
      server: {
        host: settings.server.host,
        http: { enabled: settings.server.http.enabled, port: settings.server.http.port },
        https: { enabled: settings.server.https.enabled, port: settings.server.https.port },
        ftp: { enabled: settings.server.ftp.enabled, port: settings.server.ftp.port },
        ftps: { enabled: settings.server.ftps.enabled, port: settings.server.ftps.port },
        webdav: { enabled: settings.server.webdav.enabled }
      },
      database: {
        driver: settings.database.driver,
        dsn: finalDsn
      },
      rootDirs: settings.rootDirs,
      allowedExtensions: settings.allowedExtensions,
      privateFiles: {
        ...settings.privateFiles,
        quotaGlobal: settings.privateQuotaGlobal,
        quotaPerUser: settings.privateQuotaUser
      },
      tempFiles: {
        ...settings.tempFiles,
        quotaGlobal: settings.tempFilesQuotaGlobal,
        quotaPerIP: settings.tempFilesQuotaPerIP
      },
      upload: settings.upload,
      preview: {
        allowMimes: settings.preview.allowMimes,
        allowExts: settings.preview.allowExts,
        maxInlineSize: formatToUnit(settings.preview.maxInlineSize),
        textChunkSize: formatToUnit(settings.preview.textChunkSize)
      },
      openApi: {
        enabled: settings.openApi.enabled,
        ipAccessMode: settings.openApi.ipAccessMode,
        ipWhitelist: settings.openApi.ipWhitelist.split('\n').filter(ip => ip.trim()),
        ipBlacklist: settings.openApi.ipBlacklist.split('\n').filter(ip => ip.trim()),
        rateLimitEnabled: settings.openApi.rateLimitEnabled,
        requestsPerMinute: settings.openApi.requestsPerMinute
      },
      index: {
        scanCronExpression: settings.scanCronExpression,
      }
    }

    const data = await ConfigApi.save(configData)
    if (data.success) {
      message.success(data.message || '配置已保存并生效')
      try {
        const optionsRes = await SystemApi.getOptions()
        if (optionsRes.data) {
          store.setConfig({
            appName: optionsRes.data.app?.name || '',
            privateStorageEnabled: optionsRes.data.privateStorage?.enabled ?? false,
            tempFilesEnabled: optionsRes.data.tempFiles?.enabled ?? true,
            ftpEnabled: optionsRes.data.ftp?.enabled ?? false,
            ftpPort: optionsRes.data.ftp?.port ?? 2121,
            webdavEnabled: optionsRes.data.webdav?.enabled ?? false,
            openApiEnabled: optionsRes.data.openApi?.enabled ?? false,
            anonymousUsername: optionsRes.data.account?.anonymous?.username || 'public'
          })
        }
      } catch (e) {
        console.error('刷新配置失败', e)
      }
      const hostChanged = originalServerConfig.value !== null &&
        settings.server.host !== originalServerConfig.value.host
      const portChanged = originalServerConfig.value !== null &&
        settings.server.http.port !== originalServerConfig.value.httpPort
      if (hostChanged || portChanged) {
        const displayHost = settings.server.host === '0.0.0.0' ? window.location.hostname : settings.server.host
        const newUrl = 'http://' + displayHost + ':' + settings.server.http.port
        dialog.warning({
          title: '服务器地址已变更',
          content: '服务已切换到新地址：' + newUrl,
          positiveText: '前往新地址',
          negativeText: '留在当前页面',
          onPositiveClick: () => {
            pollServerHealth(newUrl, 20, 500, () => {
              window.location.href = newUrl
            })
          }
        })
      }
    } else {
      message.error(data.message || '保存失败')
    }
  } catch (e) {
    console.error('保存设置失败', e)
    message.error('保存设置失败')
  } finally {
    saving.value = false
  }
}

// 保存配置入口（检测数据库变更）
const saveSettings = () => {
  if (detectDatabaseChange()) {
    dialog.warning({
      title: '数据库配置已变更',
      content: '检测到数据库配置已修改，是否需要迁移数据到新数据库？\n\n• 迁移数据：将原数据库迁移到新配置（推荐）\n• 仅保存配置：直接保存配置，不迁移数据（需要手动迁移）\n• 取消：不保存任何更改',
      positiveText: '迁移数据',
      negativeText: '仅保存配置',
      onPositiveClick: () => {
        showMigrationWizard.value = true
      },
      onNegativeClick: () => {
        doSaveSettings()
      }
    })
  } else {
    doSaveSettings()
  }
}

onMounted(() => {
  loadSettings()
  loadNetworkInterfaces()
})
</script>

<style lang="less" scoped>
@import '@/styles/variables.less';

.settings-view {
  padding: @container-padding;
  animation: slideUp 0.4s ease-out;

  .header-section {
    margin-bottom: 24px;

    h2 {
      margin: 0 0 8px 0;
      font-size: @font-size-xxl;
      font-weight: 600;
    }

    .description {
      margin: 0;
      color: @text-color-secondary;
      font-size: @font-size-base;
    }
  }

  .el-card {
    transition: transform @transition-smooth, box-shadow @transition-smooth;
    height: 100%;

    &:hover {
      box-shadow: @shadow-md;
    }
  }

  .el-button:not(.actions-inner .el-button) {
    transition: transform @transition-smooth, box-shadow @transition-smooth;

    &:hover:not(:disabled) {
      transform: translateY(-1px);
      box-shadow: @button-hover-shadow;
    }

    &:active:not(:disabled) {
      transform: scale(0.97);
    }
  }

  .actions-inner {
    display: flex;
    justify-content: flex-end;
    gap: 12px;
  }

  .actions-bar {
    margin-top: 24px;
    padding: 16px;
    background: @bg-color-secondary;
    border-radius: @border-radius-lg;
    position: sticky;
    bottom: 16px;
    box-shadow: @shadow-md;
  }

  .field-hint {
    color: #999;
    font-size: 12px;
  }

  .migration-hint {
    margin-bottom: 0;

    .migration-title {
      font-weight: 600;
      color: var(--primary-color);
    }

    .migration-list {
      margin: 8px 0 0 0;
      padding-left: 20px;
      color: var(--text-color-secondary);

      li {
        margin-bottom: 4px;
        line-height: 1.6;
      }
    }
  }

  .test-result {
    margin-left: 12px;
    font-size: 14px;

    &.success {
      color: #52c41a;
    }

    &.error {
      color: #ff4d4f;
    }
  }
}

@media @tablet {
  .settings-view {
    padding: 12px;
  }
}

@media @mobile {
  .settings-view {
    padding: 8px;
  }

  .actions-bar {
    padding: 12px !important;
    position: static !important;

    .actions-inner {
      justify-content: stretch;

      .el-button {
        flex: 1;
      }
    }
  }
}
</style>