import Form from 'hew/Form';
import Input from 'hew/Input';
import { Modal } from 'hew/Modal';
import React, { useId } from 'react';

import { patchWebhook } from 'services/api';
import { Webhook } from 'types';
import handleError, { ErrorLevel, ErrorType } from 'utils/error';

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

  const handleSubmit = async () => {
    if (!webhook) return;
    const values = await form.validateFields();
    const url = values.url;
    
    try {
      await patchWebhook({ url, id: webhook.id });
      onSuccess?.();
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
        form: idPrefix + FORM_ID,
        handleError,
        handler: handleSubmit,
        text: 'Save Changes',
      }}
      title="Edit Webhook">
      <Form autoComplete="off" form={form} id={idPrefix + FORM_ID} layout="vertical">
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
  