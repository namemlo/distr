import {Component, input} from '@angular/core';
import {FontAwesomeModule} from '@fortawesome/angular-fontawesome';
import {faCodeBranch, faDatabase} from '@fortawesome/free-solid-svg-icons';
import {ConfigAsCodeAuthority} from '../../types/config-as-code';

@Component({
  selector: 'app-config-as-code-authority-badge',
  templateUrl: './config-as-code-authority-badge.component.html',
  imports: [FontAwesomeModule],
})
export class ConfigAsCodeAuthorityBadgeComponent {
  readonly authority = input.required<ConfigAsCodeAuthority>();

  protected readonly faCodeBranch = faCodeBranch;
  protected readonly faDatabase = faDatabase;
}
