import {Component, input} from '@angular/core';
import {AccessTokenWithKey} from '@distr-sh/distr-sdk';
import {ClipComponent} from '../components/clip.component';

@Component({
  selector: 'app-created-access-token-alert',
  imports: [ClipComponent],
  template: `<div
    class="p-4 mx-4 mb-4 text-sm text-green-800 rounded-lg bg-green-50 dark:bg-gray-700 dark:text-green-400"
    role="alert">
    <p>
      Access Token:
      <code class="select-all" data-ph-mask-text="true">{{ token().key }}</code>
      <app-clip class="mx-2" [clip]="token().key" />
    </p>
    <p>
      <strong>Important:</strong>
      This is the only time you will be able to see this token, so please make sure to note it down before closing this
      page.
    </p>
  </div>`,
})
export class CreatedAccessTokenAlertComponent {
  public readonly token = input.required<AccessTokenWithKey>();
}
