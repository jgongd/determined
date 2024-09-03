import Form from 'hew/Form';
import Input from 'hew/Input';
import { Modal } from 'hew/Modal';
import React, { useId, useCallback, useState } from 'react';

import { paths } from 'routes/utils';
import { patchWebhook } from 'services/api';
import { Webhook } from 'types';
import handleError, { ErrorLevel, ErrorType } from 'utils/error';
import { routeToReactUrl } from 'utils/routes';

const FORM_ID = 'edit-webhook-form';

interface FormInputs {
  url: string;
}

interface Props {
  onSuccess?: () => void;
  webhook?: Webhook;
}

const WebhookEditModalComponent: React.FC<Props> = ({ onSuccess, webhook }: Props) => {
  const idPrefix = useId();
  const [form] = Form.useForm<FormInputs>();
  const [disabled, setDisabled] = useState<boolean>(true);

  const onChange = useCallback(() => {
    const fields = form.getFieldsError();
    const hasError = fields.some((f) => f.errors.length);
    setDisabled(hasError);
  }, [form]);

  const handleSubmit = async () => {
    if (!webhook) return;
    const values = await form.validateFields();
    const url = values.url;
    
    try {
      await patchWebhook({ url: url, id: webhook.id });
      onSuccess?.();
      routeToReactUrl(paths.webhooks());
    } catch (e) {
      handleError(e, {
        level: ErrorLevel.Error,
        publicMessage: 'Please try again later.',
        publicSubject: 'Unable to edit webhook.',
        silent: false,
        type: ErrorType.Server,
      });
    }
  };

  return (
    <Modal
      cancel
      size="small"
      submit={{
        disabled,
        form: idPrefix + FORM_ID,
        handleError,
        handler: handleSubmit,
        text: 'Save Changes',
      }}
      title="Edit Webhook">
      <Form autoComplete="off" form={form} id={idPrefix + FORM_ID} layout="vertical" onFieldsChange={onChange}>
        <Form.Item
          label="URL"
          name="url"
          rules={[
            { message: 'URL is required.', required: true },
            { message: 'URL must be valid.', type: 'url', whitespace: true },
          ]}>
            <Input />
        </Form.Item>
      </Form>
    </Modal>
  );
};
  
export default WebhookEditModalComponent;
  