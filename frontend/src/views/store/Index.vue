<script setup lang="ts">
import { Alert, Card, Empty, Typography, Space } from 'ant-design-vue'

/**
 * 应用商店（8091 原型第一页，也是唯一一页）。
 *
 * **本轮不实现，理由是范围不是工作量**：`_naming.yaml` 把「模板」排在批次 5
 * （部署与应用商店），ADR-043 明确只交付批次 4 + 其前置。模板要连着
 * `model/template.yaml` + 模板仓库（拉取/校验/版本）一起落，缺了仓库
 * 画出来的卡片就只是假数据——部署按钮点了不会有任何东西被创建。
 *
 * 所以这一页说清缺什么、什么时候做，而不是摆一排点不动的模板卡。
 */
</script>

<template>
  <div class="store-page">
    <Card size="small">
      <Empty description="应用商店尚未实现" />
      <Alert
        type="info"
        show-icon
        message="缺的是后端 template kind 与模板仓库，不是界面"
        description="模板模型见 docs/v4.1/model（template → 生成部署单元 + 服务/路由默认值）。落地后本页展示模板卡片、版本与一键部署。"
      />
      <div style="margin-top: 12px">
        <Typography.Paragraph type="secondary" style="font-size: 12px; margin-bottom: 4px">
          排期：批次 5「部署」（_naming.yaml：模板），本轮 ADR-043 只覆盖批次 4。
        </Typography.Paragraph>
        <Space size="4" wrap>
          <Typography.Text code style="font-size: 12px">model/template.yaml</Typography.Text>
          <Typography.Text code style="font-size: 12px">docs/v4.1/blueprint.md §6</Typography.Text>
          <Typography.Text code style="font-size: 12px">docs/adr/ADR-043.md</Typography.Text>
        </Space>
      </div>
    </Card>
  </div>
</template>

<style scoped>
.store-page {
  display: flex;
  flex-direction: column;
  gap: var(--gb-space-lg);
}
</style>