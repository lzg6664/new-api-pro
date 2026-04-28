/*
Copyright (C) 2025 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/

import React, { useEffect, useMemo, useState } from 'react';
import {
  Banner,
  Button,
  Card,
  Col,
  Input,
  Modal,
  Row,
  Select,
  Space,
  Spin,
  TextArea,
  Typography,
} from '@douyinfe/semi-ui';
import { useTranslation } from 'react-i18next';
import { API, copy, showError, showSuccess } from '../../../../helpers';
import { IconCopy } from '@douyinfe/semi-icons';

const { Text } = Typography;

const buildDefaultBody = (model) =>
  JSON.stringify(
    {
      model: model || 'gpt-4o-mini',
      messages: [
        {
          role: 'user',
          content: 'Hello',
        },
      ],
    },
    null,
    2,
  );

const prettyJson = (raw) => {
  const text = String(raw ?? '').trim();
  if (!text) return '';
  try {
    return JSON.stringify(JSON.parse(text), null, 2);
  } catch {
    return raw;
  }
};

const RequestPreviewModal = ({
  visible,
  channelId,
  defaultModel,
  onCancel,
}) => {
  const { t } = useTranslation();
  const [method, setMethod] = useState('POST');
  const [requestPath, setRequestPath] = useState('/v1/chat/completions');
  const [headersText, setHeadersText] = useState(
    JSON.stringify(
      {
        'content-type': 'application/json',
      },
      null,
      2,
    ),
  );
  const [bodyText, setBodyText] = useState(buildDefaultBody(defaultModel));
  const [loading, setLoading] = useState(false);
  const [result, setResult] = useState(null);

  useEffect(() => {
    if (!visible) return;
    setMethod('POST');
    setRequestPath('/v1/chat/completions');
    setHeadersText(
      JSON.stringify(
        {
          'content-type': 'application/json',
        },
        null,
        2,
      ),
    );
    setBodyText(buildDefaultBody(defaultModel));
    setResult(null);
  }, [defaultModel, visible]);

  const prettyHeaders = useMemo(
    () => prettyJson(result?.headers ? JSON.stringify(result.headers) : ''),
    [result],
  );
  const prettyBody = useMemo(() => prettyJson(result?.body || ''), [result]);
  const auditText = useMemo(
    () => (result?.param_override_audit || []).join('\n'),
    [result],
  );

  const handlePreview = async () => {
    let parsedHeaders = {};
    try {
      parsedHeaders = headersText.trim() ? JSON.parse(headersText) : {};
    } catch (error) {
      showError(t('请求头必须是合法的 JSON 对象'));
      return;
    }
    if (
      !parsedHeaders ||
      typeof parsedHeaders !== 'object' ||
      Array.isArray(parsedHeaders)
    ) {
      showError(t('请求头必须是合法的 JSON 对象'));
      return;
    }

    setLoading(true);
    try {
      const res = await API.post(`/api/channel/${channelId}/request-preview`, {
        request_path: requestPath,
        method,
        headers: parsedHeaders,
        body: bodyText,
      });
      const { success, message, data } = res.data;
      if (!success) {
        showError(message);
        return;
      }
      setResult(data);
    } catch (error) {
      showError(t('请求预览失败'));
    } finally {
      setLoading(false);
    }
  };

  const handleCopy = async (value, successText) => {
    if (!value) {
      showError(t('没有可复制的内容'));
      return;
    }
    const ok = await copy(value);
    if (ok) {
      showSuccess(successText);
    } else {
      showError(t('复制失败'));
    }
  };

  return (
    <Modal
      title={t('请求预览')}
      visible={visible}
      onCancel={onCancel}
      footer={
        <Space>
          <Button onClick={onCancel}>{t('关闭')}</Button>
          <Button type='primary' loading={loading} onClick={handlePreview}>
            {t('生成预览')}
          </Button>
        </Space>
      }
      width={980}
      centered
    >
      <Spin spinning={loading}>
        <div className='space-y-3'>
          <Banner
            type='info'
            description={t(
              '当前仅支持 JSON 请求体预览。multipart、音频上传、WebSocket 和异步任务请求暂不支持。',
            )}
          />

          <Row gutter={12}>
            <Col span={6}>
              <Text className='mb-1 block'>{t('请求方法')}</Text>
              <Select
                value={method}
                onChange={setMethod}
                optionList={[
                  { label: 'POST', value: 'POST' },
                  { label: 'GET', value: 'GET' },
                  { label: 'PUT', value: 'PUT' },
                  { label: 'PATCH', value: 'PATCH' },
                  { label: 'DELETE', value: 'DELETE' },
                ]}
              />
            </Col>
            <Col span={18}>
              <Text className='mb-1 block'>{t('请求路径')}</Text>
              <Input
                value={requestPath}
                onChange={setRequestPath}
                placeholder='/v1/chat/completions'
                showClear
              />
            </Col>
          </Row>

          <Card>
            <Text className='mb-2 block font-medium'>{t('测试请求头')}</Text>
            <TextArea
              value={headersText}
              onChange={setHeadersText}
              autosize={{ minRows: 4, maxRows: 10 }}
            />
          </Card>

          <Card>
            <Text className='mb-2 block font-medium'>{t('测试请求体')}</Text>
            <TextArea
              value={bodyText}
              onChange={setBodyText}
              autosize={{ minRows: 10, maxRows: 20 }}
            />
          </Card>

          {result && (
            <>
              <Card>
                <div className='flex items-center justify-between mb-2'>
                  <Text className='font-medium'>{t('Route / URL')}</Text>
                  <Space>
                    <Button
                      size='small'
                      icon={<IconCopy size={14} />}
                      onClick={() =>
                        handleCopy(result.final_url, t('最终 URL 已复制'))
                      }
                    >
                      {t('复制 URL')}
                    </Button>
                  </Space>
                </div>
                <div className='text-sm space-y-1'>
                  <div>
                    <Text type='tertiary'>{t('命中路由')}:</Text>{' '}
                    <Text>{result.matched_route || t('未命中')}</Text>
                  </div>
                  <div>
                    <Text type='tertiary'>{t('最终方法')}:</Text>{' '}
                    <Text>{result.final_method}</Text>
                  </div>
                  <div className='break-all'>
                    <Text type='tertiary'>{t('最终 URL')}:</Text>{' '}
                    <Text>{result.final_url}</Text>
                  </div>
                </div>
              </Card>

              <Card>
                <div className='flex items-center justify-between mb-2'>
                  <Text className='font-medium'>{t('最终请求头')}</Text>
                  <Button
                    size='small'
                    icon={<IconCopy size={14} />}
                    onClick={() =>
                      handleCopy(prettyHeaders, t('最终请求头已复制'))
                    }
                  >
                    {t('复制')}
                  </Button>
                </div>
                <pre className='mb-0 text-xs leading-5 whitespace-pre-wrap break-all max-h-64 overflow-auto'>
                  {prettyHeaders || '{}'}
                </pre>
              </Card>

              <Card>
                <div className='flex items-center justify-between mb-2'>
                  <Text className='font-medium'>{t('最终请求体')}</Text>
                  <Button
                    size='small'
                    icon={<IconCopy size={14} />}
                    onClick={() =>
                      handleCopy(prettyBody, t('最终请求体已复制'))
                    }
                  >
                    {t('复制')}
                  </Button>
                </div>
                <pre className='mb-0 text-xs leading-5 whitespace-pre-wrap break-all max-h-80 overflow-auto'>
                  {prettyBody || ''}
                </pre>
              </Card>

              {(result.param_override_audit || []).length > 0 && (
                <Card>
                  <Text className='mb-2 block font-medium'>
                    {t('参数覆盖审计')}
                  </Text>
                  <pre className='mb-0 text-xs leading-5 whitespace-pre-wrap break-all max-h-48 overflow-auto'>
                    {auditText}
                  </pre>
                </Card>
              )}
            </>
          )}
        </div>
      </Spin>
    </Modal>
  );
};

export default RequestPreviewModal;
